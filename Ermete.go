package main

import (
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"
    "strings"
	"fmt"
    "math"

    "github.com/bwmarrin/discordgo"
    "github.com/gordonklaus/portaudio"
    "github.com/hraban/opus"
)

var (
    commandPrefix    = "gl."
    botToken         = os.Getenv("GOLIVE_BOT_TOKEN")
    ownerList        []string
    shutdownChan     = make(chan struct{})
    inputDevice      ErmeteInput  = &PortAudioInput{}
    outputDevice     ErmeteOutput = &PortAudioOutput{}
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
    stream      *portaudio.Stream
    vc          *discordgo.VoiceConnection
    opusDecoder *opus.Decoder
    audioChan   chan []float32
    buffer      []float32 // Pre-allocated buffer for output
    bufferSize  int
}

func main() {
    err := portaudio.Initialize()
    if err != nil {
        log.Fatalf("PortAudio initialization failed: %v", err)
    }
    defer portaudio.Terminate()
	
	devices, err := portaudio.Devices()
	if err != nil {
    	log.Fatal("Error listing PortAudio devices:", err)
	}
	for i, device := range devices {
   	 log.Printf("Device %d: Name: %s, Input Channels: %d, Output Channels: %d", i, device.Name, device.MaxInputChannels, device.MaxOutputChannels)
	}

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

func (pi *PortAudioInput) Start(vc *discordgo.VoiceConnection) error {
    pi.vc = vc
    pi.audioChan = make(chan []float32, 100)
    var err error
    pi.opusEncoder, err = opus.NewEncoder(48000, 2, opus.AppAudio)
    if err != nil {
        return err
    }
    pi.opusEncoder.SetBitrate(96000)
    inputDevice, err := portaudio.DefaultInputDevice()
    if err != nil {
        return err
    }

    inputParams := portaudio.StreamParameters{
        Input:         portaudio.StreamDeviceParameters{Device: inputDevice, Channels: 2},
        SampleRate:    48000,
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
        encoded := make([]byte, 2048)

        n, err := pi.opusEncoder.EncodeFloat32(frame, encoded)
        if err != nil {
            log.Println("Error encoding audio:", err)
            continue
        }

        select {
        case pi.vc.OpusSend <- encoded[:n]: // Send encoded data
            time.Sleep(5 * time.Millisecond)
        default:
            log.Println("OpusSend channel is full, dropping frame")
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

// Helper function to get default output stream parameters
func DefaultOutputStreamParameters(device *portaudio.DeviceInfo, channels, sampleRate int) (portaudio.StreamParameters, error) {
    if device == nil {
        return portaudio.StreamParameters{}, fmt.Errorf("portaudio: invalid device") // Use fmt.Errorf
    }

    if device.MaxOutputChannels < channels {
        return portaudio.StreamParameters{}, fmt.Errorf("portaudio: invalid channel count") // Use fmt.Errorf
    }

    return portaudio.StreamParameters{
        Output: portaudio.StreamDeviceParameters{
            Device:   device,
            Channels: channels,
        },
        SampleRate:    float64(sampleRate),
        FramesPerBuffer: portaudio.FramesPerBufferUnspecified, // Let PortAudio choose a good buffer size
    }, nil
}

func (po *PortAudioOutput) Start(vc *discordgo.VoiceConnection) error {
    po.vc = vc

    var err error
    po.opusDecoder, err = opus.NewDecoder(48000, 1) // Decode as mono (1 channel)
    if err != nil {
        return err
    }

    // Calculate buffer size based on frames per buffer (960) and channels (2)
	po.bufferSize = 960 * 2
    po.buffer = make([]float32, po.bufferSize) // Pre-allocate the buffer
	po.audioChan = make(chan []float32, 1000) // was 100
	
	outputDevice, err := portaudio.DefaultOutputDevice() // Get both values
    if err != nil {
        return err // Handle the error from DefaultOutputDevice
    }

	outputParams, err := DefaultOutputStreamParameters(outputDevice, 2, 48000) // Get default stream parameters
	if err != nil {
		return err
	}

	outputParams.FramesPerBuffer = 960 // Now modify FramesPerBuffer

	po.stream, err = portaudio.OpenStream(outputParams, po.callback) // Use the modified outputParams
	if err != nil {
		return err
	}


    err = po.stream.Start()
    if err != nil {
        return err
    }

    go po.receiveAudio(vc)

    return nil
}

func (po *PortAudioOutput) receiveAudio(vc *discordgo.VoiceConnection) { // Added receiver (po *PortAudioOutput)
    for {
        select {
        case pkt, ok := <-vc.OpusRecv:
            if !ok {
                log.Println("OpusRecv channel closed")
                return
            }

            opusData := pkt.Opus
			// uncomment for debugging
            // log.Printf("Received Opus packet length: %d", len(opusData))

//            decoded := make([]int16, 960*2) // If Discord was Stereo!
			decoded := make([]int16, 960) // If Discord mono
            n, err := po.opusDecoder.Decode(opusData, decoded)
            if err != nil {
                log.Printf("Decode error: opus: corrupted stream: %v, skipping frame: %v", err, opusData)
                continue
            }

            floatData := make([]float32, n) // Use actual number of decoded samples
            for i := 0; i < n; i++ {
                floatData[i] = float32(decoded[i]) / 32767.0
            }

            if po.audioChan != nil { // Check if audioChan is initialized
                select {
                case po.audioChan <- floatData:
                default:
                    log.Println("Audio channel full, dropping frame")
                }
            } else {
                log.Println("Audio channel is nil, cannot send data")
            }

        case <-shutdownChan:
            log.Println("Shutting down audio receiver")
            return
        }
    }
}

func (po *PortAudioOutput) processAudio(data []float32, out []float32) []float32 {
    stereoData := make([]float32, len(out)) // Stereo output buffer

    // Convert mono to stereo: duplicate samples
    for i := 0; i < len(data); i++ {
        stereoData[i*2] = data[i]       // Left channel
        stereoData[i*2+1] = data[i]     // Right channel
    }

    // Compressor parameters (adjust these)
    threshold := float32(0.5)  // Threshold above which compression is applied
    ratio := float32(4.0)     // Compression ratio (higher ratio = more compression)

    processed := make([]float32, len(stereoData)) // Create a copy for the stereo data

    for i := 0; i < len(stereoData); i++ { // Iterate over the stereo data
        sample := stereoData[i]
        absSample := float32(math.Abs(float64(sample)))

        gain := float32(1.0)
        if absSample > threshold {
            gain = threshold + (absSample-threshold)/ratio
        }

        processed[i] = sample * gain
    }

    copy(out, processed) // Copy the compressed stereo data to the output buffer
    return out
}

func (po *PortAudioOutput) callback(out []float32) {
    select {
    case data := <-po.audioChan:
        processedData := po.processAudio(data, out) // Call processAudio
        copy(out, processedData)                   // Copy to output buffer
    default:
        // Fill with silence (important!)
        for i := range out {
            out[i] = 0
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
    botToken = os.Getenv("GOLIVE_BOT_TOKEN")
    if botToken == "" {
        log.Fatal("Bot token not set. Please set GOLIVE_BOT_TOKEN environment variable.")
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

    log.Println("Stopping audio processing before leaving the voice channel.")

    // Stop the input/output processing before leaving
    if err := inputDevice.Stop(); err != nil {
        log.Printf("Error stopping input device: %v", err)
    }
    if err := outputDevice.Stop(); err != nil {
        log.Printf("Error stopping output device: %v", err)
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
