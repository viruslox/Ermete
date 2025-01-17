package main

import (
	"fmt"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"
    "strings"
	"math"

    "github.com/bwmarrin/discordgo"
    "github.com/gordonklaus/portaudio"
    "gopkg.in/hraban/opus.v2"
)

var (
    commandPrefix      string // Declare at package level
    botToken           string // Declare at package level
    inputDeviceName    string // Declare at package level
    outputDeviceName   string // Declare at package level
    ownerList          []string
    shutdownChan       = make(chan struct{})
    inputDevice        ErmeteInput  = &PortAudioInput{}
    outputDevice       ErmeteOutput = &PortAudioOutput{}
)

var commandHandlers = map[string]func(*discordgo.Session, *discordgo.MessageCreate){
    "join":    handleJoinCommand,
    "leave":   handleLeaveCommand,
    "shutdown": handleShutdownCommand,
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
    audioChan   chan []float32 // Buffered channel for audio data
}

type PortAudioOutput struct {
    stream          *portaudio.Stream
    vc              *discordgo.VoiceConnection
    opusDecoder     *opus.Decoder
    audioChan       chan []float32
    bufferSize      int
    decoderReciprocal float32
	lastData  []float32
}


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

    // Correct way to list devices
    devices, err := portaudio.Devices()
    if err != nil {
        log.Fatalf("Could not get devices: %v", err)
    }

    fmt.Println("Available PortAudio Devices:") // Now fmt is defined
    for i, device := range devices {
        fmt.Printf("  #%d: %s (Input Channels: %d, Output Channels: %d, Default Sample Rate: %.2f)\n", // fmt here as well
            i, device.Name, device.MaxInputChannels, device.MaxOutputChannels, device.DefaultSampleRate)
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
        log.Println("Received OS shutdown signal (Ctrl+C).")
    case <-shutdownChan:
        log.Println("Received Discord shutdown command.")
    }

    // *** Shutdown Logic Moved Here ***
    log.Println("Starting bot shutdown...")

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
        log.Println("Timeout reached during device cleanup, force shutting down...")
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

func normalize(data []float32) {
    maxValue := float32(0)
    for _, val := range data {
        absVal := float32(math.Abs(float64(val)))
        if absVal > maxValue {
            maxValue = absVal
        }
    }

    if maxValue > 0 { // Avoid division by zero
        scaleFactor := 1.0 / maxValue
        for i := range data {
            data[i] *= scaleFactor
        }
    }
}

func (pi *PortAudioInput) Start(vc *discordgo.VoiceConnection) error {
    pi.vc = vc
    pi.audioChan = make(chan []float32, 30) // Buffered channel for audio frames

    var err error
	pi.opusEncoder, err = opus.NewEncoder(48000, 2, opus.AppAudio)
	if err != nil {
		return err
	}

	err = pi.opusEncoder.SetBitrate(64000) // 64 kbps
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
        SampleRate:      48000,
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

    // Start a goroutine to consume from the channel and send to OpusSend
    go pi.processAudio()

    return nil
}

func (pi *PortAudioInput) callback(in []float32, out []float32) {
    buffer := make([]float32, len(in))
    copy(buffer, in)

    select {
    case pi.audioChan <- buffer:
        // Successfully added to buffer
    default:
        log.Println("Audio buffer is full, dropping frame")
    }
}

func (pi *PortAudioInput) processAudio() {
    for frame := range pi.audioChan {
        encoded := make([]byte, 4096)

        n, err := pi.opusEncoder.EncodeFloat32(frame, encoded)
        if err != nil {
            log.Println("Error encoding audio:", err)
            continue
        }

        select {
		case pi.vc.OpusSend <- encoded[:n]: // Send encoded data
		default:
    		log.Println("processAudio: OpusSend channel is full, dropping frame")
		}
    }
}


func (pi *PortAudioInput) Stop() error {
    if pi.stream != nil {
        close(pi.audioChan) // Close the audio channel
        return pi.stream.Close()
    }
    return nil
}

func (po *PortAudioOutput) Start(vc *discordgo.VoiceConnection) error {
    po.vc = vc // Assign the voice connection

    var err error
    po.opusDecoder, err = opus.NewDecoder(48000, 2)
    if err != nil {
        return err
    }

    po.audioChan = make(chan []float32, 10) // Create buffered channel

    outputDevice, err := portaudio.OpenDefaultStream(0, 2, 44100, 0, po.callback) // Provide the callback
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
    for pkt := range vc.OpusRecv {
        opusData := pkt.Opus
        decoded := make([]int16, 4096)

        n, err := po.opusDecoder.Decode(opusData, decoded)
        if err != nil {
            log.Printf("Decode error: %v", err)
            continue
        }
// Enable this Log for debugging
//           log.Println("Received audio packet from Discord") 
            
        floatData := make([]float32, n)
        for i := 0; i < n; i++ {
            floatData[i] = float32(decoded[i]) / 32767.0
        }

        po.audioChan <- floatData // Pass decoded audio to callback
    }
}

func (po *PortAudioOutput) callback(out []float32) {
    select {
    case data := <-po.audioChan:
        copy(out, data)
    default:
        for i := range out {
            out[i] = 0 // Fill with silence
        }
    }
}

func (po *PortAudioOutput) Stop() error {
    if po.stream != nil {
        close(po.audioChan)
        return po.stream.Close()
    }
    return nil
}

func init() {
    commandPrefix = os.Getenv("GOLIVE_BOT_PREFIX")
    if commandPrefix == "" {
        log.Fatal("Bot command prefix not set. Please set GOLIVE_BOT_PREFIX environment variable.")
    }
	
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

    if !strings.HasPrefix(m.Content, commandPrefix) {
        return
    }

    command := strings.TrimPrefix(m.Content, commandPrefix)
    args := strings.Fields(command)
    if len(args) == 0 {
        return
    }

    if handler, exists := commandHandlers[args[0]]; exists {
        handler(s, m)
    } else {
        log.Println("Unknown command:", args[0])
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
