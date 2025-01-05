package main

import (
    "log"
    "os"
    "os/signal"
    "runtime"
    "syscall"
    "time"

    "github.com/bwmarrin/discordgo"
    "github.com/gordonklaus/portaudio"
    "github.com/hraban/opus"
)

var (
    commandPrefix    = "gl."
    botToken         = os.Getenv("GOLIVE_BOT_TOKEN")
    outputDeviceName = os.Getenv("ERMETE_OUTPUT_DEVICE")
    ownerList        []string
    shutdownChan     = make(chan struct{})
    inputDevice      ErmeteInput  = &PortAudioInput{}
    outputDevice     ErmeteOutput = &PortAudioOutput{}
)

func main() {
    err := portaudio.Initialize()
    if err != nil {
        log.Fatalf("PortAudio initialization failed: %v", err)
    }
    defer portaudio.Terminate()

    dg, err := discordgo.New("Bot " + botToken)
    if err != nil {
        log.Fatalf("Error creating Discord session: %v", err)
    }

    dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentGuildVoiceStates | discordgo.IntentsGuildMembers

    dg.AddHandler(onReady)
    dg.AddHandler(onMessageCreate)
    dg.AddHandler(onVoiceStateUpdate)

    err = dg.Open()
    if err != nil {
        log.Fatalf("Error opening Discord session: %v", err)
    }
    defer dg.Close()

    log.Println("Bot is now running. Press CTRL+C to exit.")

    shutdown := make(chan os.Signal, 1)
    signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

    select {
    case <-shutdown:
        log.Println("Received shutdown signal, closing bot...")

        timeout := time.After(5 * time.Second)

        stopErr := make(chan error, 2)
        go func() {
            if err := inputDevice.Stop(); err != nil {
                stopErr <- err
            } else {
                stopErr <- nil
            }
        }()
        go func() {
            if err := outputDevice.Stop(); err != nil {
                stopErr <- err
            } else {
                stopErr <- nil
            }
        }()

        select {
        case err := <-stopErr:
            if err != nil {
                log.Printf("Error stopping devices: %v", err)
            }
        case <-timeout:
            log.Println("Timeout reached during cleanup, force shutting down...")
        }

        for _, vc := range dg.VoiceConnections {
            if vc != nil {
                if err := vc.Disconnect(); err != nil {
                    log.Printf("Error disconnecting from voice channel: %v", err)
                }
            }
        }

        time.Sleep(2 * time.Second)
        log.Println("Bot shutdown complete.")
    }
}

type ErmeteInput interface {
    Start(vc *discordgo.VoiceConnection) error
    Stop() error
}

type ErmeteOutput interface {
    Start(vc *discordgo.VoiceConnection) error
    Stop() error
}

type PortAudioInput struct {
    stream      *portaudio.Stream
    vc          *discordgo.VoiceConnection
    opusEncoder *opus.Encoder
}

func (pi *PortAudioInput) Start(vc *discordgo.VoiceConnection) error {
    pi.vc = vc

    var err error
    pi.opusEncoder, err = opus.NewEncoder(48000, 2, opus.AppAudio)
    if err != nil {
        return err
    }

    inputDevice, err := portaudio.DefaultInputDevice()
    if err != nil {
        return err
    }

    inputParams := portaudio.StreamParameters{
        Input: portaudio.StreamDeviceParameters{
            Device:   inputDevice,
            Channels: 2,
        },
        SampleRate:      44100,
        FramesPerBuffer: 960,
    }

    pi.stream, err = portaudio.OpenStream(inputParams, pi.callback)
    if err != nil {
        return err
    }

    err = pi.stream.Start()
    if err != nil {
        return err
    }

    return nil
}

func (pi *PortAudioInput) callback(in []float32, out []float32) {
    buffer := make([]float32, len(in))
    copy(buffer, in)

    encoded := make([]byte, 4096) // Assure encoded is of type []byte with sufficient size

    n, err := pi.opusEncoder.EncodeFloat32(buffer, encoded)
    if err != nil {
        log.Println("Error encoding audio:", err)
        return
    }

    // Add a small delay
    time.Sleep(10 * time.Millisecond)

    pi.vc.OpusSend <- encoded[:n] // Use only the filled part of encoded
}

func (pi *PortAudioInput) Stop() error {
    if pi.stream != nil {
        return pi.stream.Close()
    }
    return nil
}

type PortAudioOutput struct {
    stream      *portaudio.Stream
    opusDecoder *opus.Decoder
    vc          *discordgo.VoiceConnection
}

func (po *PortAudioOutput) Start(vc *discordgo.VoiceConnection) error {
    po.vc = vc

    var err error
    po.opusDecoder, err = opus.NewDecoder(48000, 2)
    if err != nil {
        return err
    }

    outputDevice, err := portaudio.OpenDefaultStream(0, 2, 44100, 0, po.callback)
    if err != nil {
        return err
    }

    po.stream = outputDevice

    err = po.stream.Start()
    if err != nil {
        return err
    }

    go po.receiveAudio(vc)

    return nil
}

func (po *PortAudioOutput) receiveAudio(vc *discordgo.VoiceConnection) {
    for {
        select {
        case pkt, ok := <-vc.OpusRecv:
            if !ok {
                return
            }

            log.Println("Received audio packet from Discord") // Log for debugging

            opusData := pkt.Opus

            decoded := make([]int16, 4096)
            n, err := po.opusDecoder.Decode(opusData, decoded)
            if err != nil {
                log.Println("Error decoding audio:", err)
                continue
            }

            output := make([]float32, n)
            for i := 0; i < n; i++ {
                output[i] = float32(decoded[i]) / 32767.0
            }

            // Write the decoded data to the output stream
            err = po.stream.Write()
            if err != nil {
                log.Println("Error writing to stream:", err)
                return
            }

            log.Println("Audio data written to stream") // Log for debugging
        }
    }
}

func (po *PortAudioOutput) callback(out []float32) {
    if po.vc == nil {
        return
    }

    buffer := make([]float32, len(out))
    copy(out, buffer)

    if time.Now().Second()%10 == 0 {
        var memStats runtime.MemStats
        runtime.ReadMemStats(&memStats)
        log.Printf("Memory usage: Alloc=%v, TotalAlloc=%v, Sys=%v, NumGC=%v", memStats.Alloc, memStats.TotalAlloc, memStats.Sys, memStats.NumGC)
    }
}

func (po *PortAudioOutput) Stop() error {
    if po.stream != nil {
        return po.stream.Close()
    }
    return nil
}

func init() {
    commandPrefix = os.Getenv("GOLIVE_BOT_PREFIX")
    botToken = os.Getenv("GOLIVE_BOT_TOKEN")
    if botToken == "" {
        log.Fatal("Bot token not set. Please set GOLIVE_BOT_TOKEN environment variable.")
    }

    inputDeviceName := os.Getenv("ERMETE_INPUT_DEVICE")
    if inputDeviceName == "" {
        log.Fatal("Input device not set. Please set ERMETE_INPUT_DEVICE environment variable.")
    }

    outputDeviceName = os.Getenv("ERMETE_OUTPUT_DEVICE")
    if outputDeviceName == "" {
        log.Fatal("Output device not set. Please set ERMETE_OUTPUT_DEVICE environment variable.")
    }
}

func onReady(s *discordgo.Session, event *discordgo.Ready) {
    log.Printf("Logged in as: %s#%s\n", event.User.Username, event.User.Discriminator)

    app, err := s.Application("@me")
    if err != nil {
        log.Printf("Error fetching bot application info: %v", err)
        return
    }

    log.Printf("Bot application info: %+v", app)

    if app.Team != nil {
        for _, member := range app.Team.Members {
            ownerList = append(ownerList, member.User.ID)
        }
    } else {
        ownerList = append(ownerList, app.Owner.ID)
    }

    log.Printf("Owner(s) of the bot: %v", ownerList)
}

func isOwner(userID string) bool {
    for _, ownerID := range ownerList {
        if ownerID == userID {
            return true
        }
    }
    return false
}

func onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
    if m.Author.Bot {
        return
    }

    log.Printf("Received message: %s", m.Content)

    switch {
    case m.Content == commandPrefix+"leave":
        log.Println("Leave command detected")
        handleLeaveCommand(s, m)
    case m.Content == commandPrefix+"shutdown":
        log.Println("Shutdown command detected")
        handleShutdownCommand(s, m)
    case len(m.Content) > len(commandPrefix+"join "):
        log.Println("Join command detected")
        handleJoinCommand(s, m)
    default:
        log.Println("No matching command found.")
    }
}

func handleJoinCommand(s *discordgo.Session, m *discordgo.MessageCreate) {
    channelID := m.Content[len(commandPrefix+"join "):]

    vc, exists := s.VoiceConnections[m.GuildID]
    if exists && vc.ChannelID == channelID {
        log.Println("Bot is already in the voice channel.")
        return
    }

    log.Printf("Attempting to join channel: %s", channelID)

    vc, err := s.ChannelVoiceJoin(m.GuildID, channelID, false, false)
    if err != nil {
        log.Printf("Error joining voice channel: %v", err)
        return
    }

    log.Println("Bot successfully joined the voice channel!")

    if err := inputDevice.Start(vc); err != nil {
        log.Printf("Error starting input device: %v", err)
        return
    }
    if err := outputDevice.Start(vc); err != nil {
        log.Printf("Error starting output device: %v", err)
        return
    }
}

func handleLeaveCommand(s *discordgo.Session, m *discordgo.MessageCreate) {
    vc, exists := s.VoiceConnections[m.GuildID]
    if !exists || vc == nil {
        log.Println("Bot is not connected to a voice channel.")
        return
    }

    log.Println("Bot is leaving the voice channel.")
    vc.Disconnect()
}

func handleShutdownCommand(s *discordgo.Session, m *discordgo.MessageCreate) {
    if !isOwner(m.Author.ID) {
        log.Println("Unauthorized shutdown attempt.")
        return
    }
    log.Println("Shutting down bot.")
    shutdownChan <- struct{}{}
}

func onVoiceStateUpdate(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
    if v.UserID == s.State.User.ID {
        if v.ChannelID == "" {
            log.Println("Bot left the voice channel.")
        } else {
            log.Printf("Bot joined the voice channel: %s", v.ChannelID)
        }
    }
}
