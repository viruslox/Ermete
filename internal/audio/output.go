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
}

var outputDevice *PortAudioOutput

func StartOutput(vc *discordgo.VoiceConnection, shutdownChan <-chan struct{}) error {
	outputDevice = &PortAudioOutput{}
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
		return portaudio.StreamParameters{}, fmt.Errorf("portaudio: invalid device")
	}
	if device.MaxOutputChannels < channels {
		return portaudio.StreamParameters{}, fmt.Errorf("portaudio: invalid channel count")
	}

	return portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   device,
			Channels: channels,
		},
		SampleRate:    float64(sampleRate),
		FramesPerBuffer: 960, // Fixed size
	}, nil
}

func (po *PortAudioOutput) receiveAudio(vc *discordgo.VoiceConnection, shutdownChan <-chan struct{}) error {
	po.vc = vc

	var err error
	po.opusDecoder, err = opus.NewDecoder(48000, 1) // Discor audio is Mono!
	if err != nil {
		return err
	}

	po.audioChan = make(chan []float32, 100) // Adjusted buffer size

	outputDevice, err := portaudio.DefaultOutputDevice()
	if err != nil {
		return err
	}

	outputParams, err := DefaultOutputStreamParameters(outputDevice, 1, 48000) // 1 channel (mono)
	if err != nil {
		return err
	}

	po.stream, err = portaudio.OpenStream(outputParams, po.callback)
	if err != nil {
		return err
	}

	if err := po.stream.Start(); err != nil {
		return err
	}

	go po.processIncomingAudio(vc, shutdownChan)

	return nil
}

func (po *PortAudioOutput) processIncomingAudio(vc *discordgo.VoiceConnection, shutdownChan <-chan struct{}) {
	decoded := make([]int16, 960) // Reuse buffer

	for {
		select {
		case pkt, ok := <-vc.OpusRecv:
			if !ok {
				log.Println("OpusRecv channel closed")
				return
			}

			n, err := po.opusDecoder.Decode(pkt.Opus, decoded)
			if err != nil {
				log.Println("Opus decode error, skipping frame")
				continue
			}

			floatData := make([]float32, n)
			for i := 0; i < n; i++ {
				floatData[i] = float32(decoded[i]) / 32767.0
			}

			select {
			case po.audioChan <- floatData:
			default:
				log.Println("Audio channel full, dropping frame")
			}

		case <-shutdownChan:
			log.Println("Shutting down audio receiver")
			return
		}
	}
}

func (po *PortAudioOutput) callback(out []float32) {
	select {
	case data := <-po.audioChan:
		po.applyCompression(data, out) // Directly modify `out`
	default:
		for i := range out {
			out[i] = 0
		}
	}
}

func (po *PortAudioOutput) applyCompression(data []float32, out []float32) {
	threshold := float32(0.5)
	ratio := float32(4.0)

	for i := range data {
		sample := data[i]
		absSample := float32(math.Abs(float64(sample)))

		gain := float32(1.0)
		if absSample > threshold {
			gain = threshold + (absSample-threshold)/ratio
		}

		out[i] = sample * gain
	}
}

func (po *PortAudioOutput) Stop() error {
	if po.stream != nil {
		close(po.audioChan)
		return po.stream.Close()
	}
	return nil
}
