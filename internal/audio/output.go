package audio

import (
        "fmt"
        "log"
        "math"

        "github.com/bwmarrin/discordgo"
        "github.com/gordonklaus/portaudio"
        "github.com/hraban/opus"
)

type PortAudioOutput struct {
        stream      *portaudio.Stream
        vc          *discordgo.VoiceConnection
        opusDecoder *opus.Decoder
        audioChan   chan []float32
        buffer      []float32
        bufferSize  int
}

var outputDevice *PortAudioOutput

func StartOutput(vc *discordgo.VoiceConnection, shutdownChan <-chan struct{}) error {
        outputDevice = &PortAudioOutput{} // Initialize here
        return outputDevice.receiveAudio(vc, shutdownChan)
}

func StopOutput() error {
        if outputDevice != nil {
                return outputDevice.Stop()
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

func (po *PortAudioOutput) receiveAudio(vc *discordgo.VoiceConnection, shutdownChan <-chan struct{}) error {
        po.vc = vc

        var err error
        po.opusDecoder, err = opus.NewDecoder(48000, 1) // Decode as mono (1 channel)
        if err != nil {
                return err
        }

        // Calculate buffer size based on frames per buffer (960) and channels (2)
        po.bufferSize = 960 * 2
        po.buffer = make([]float32, po.bufferSize) // Pre-allocate the buffer
        po.audioChan = make(chan []float32, 1000)   // was 100

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

        go po.processIncomingAudio(vc, shutdownChan)

        return nil
}

func (po *PortAudioOutput) processIncomingAudio(vc *discordgo.VoiceConnection, shutdownChan <-chan struct{}) { // Added receiver (po *PortAudioOutput)
        for {
                select {
                case pkt, ok := <-vc.OpusRecv:
                        if !ok {
                                log.Println("OpusRecv channel closed")
                                return
                        }

                        opusData := pkt.Opus

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

                case <-shutdownChan: // Use the passed in shutdown channel
                        log.Println("Shutting down audio receiver")
                        return
                }
        }
}

func (po *PortAudioOutput) processAudio(data []float32, out []float32) []float32 {
        stereoData := make([]float32, len(out)) // Stereo output buffer

        // Convert mono to stereo: duplicate samples
        for i := 0; i < len(data); i++ {
                stereoData[i*2] = data[i]   // Left channel
                stereoData[i*2+1] = data[i] // Right channel
        }

        // Compressor parameters (adjust these)
        threshold := float32(0.5) // Threshold above which compression is applied
        ratio := float32(4.0)    // Compression ratio (higher ratio = more compression)

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
