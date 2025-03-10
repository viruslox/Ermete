package audio

import (
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
	shutdown    chan struct{}
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

func (po *PortAudioOutput) receiveAudio(vc *discordgo.VoiceConnection, shutdownChan <-chan struct{}) error {
	var err error
	po.vc = vc
	po.opusDecoder, err = opus.NewDecoder(48000, 1)
	if err != nil {
		return err
	}

	po.audioChan = make(chan []float32, 20) // Small buffer to prevent real-time dropouts
	po.shutdown = make(chan struct{})

	device, err := portaudio.DefaultOutputDevice()
	if err != nil {
		return err
	}

	params := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   device,
			Channels: 1,
		},
		SampleRate:      48000,
		FramesPerBuffer: 960,
	}

	po.stream, err = portaudio.OpenStream(params, po.callback)
	if err != nil {
		return err
	}

	if err := po.stream.Start(); err != nil {
		return err
	}

	go po.processIncomingAudio(shutdownChan)
	return nil
}

func (po *PortAudioOutput) processIncomingAudio(shutdownChan <-chan struct{}) {
	defer log.Println("AudioOut processing stopped")
	decoded := make([]int16, 960)

	for {
		select {
		case pkt, ok := <-po.vc.OpusRecv:
			if !ok {
				log.Println("AudioOut: OpusRecv channel closed")
				return
			}

			n, err := po.opusDecoder.Decode(pkt.Opus, decoded)
			if err != nil {
				log.Println("AudioOut: Dropping corrupted Opus packet")
				continue
			}

			floatData := make([]float32, n)
			for i := 0; i < n; i++ {
				floatData[i] = float32(decoded[i]) / 32767.0
			}

			select {
			case po.audioChan <- floatData:
			default:
				log.Println("AudioOut: Audio buffer full, dropping frame")
			}

		case <-shutdownChan:
			log.Println("AudioOut: Shutting down receiver")
			return
		case <-po.shutdown:
			log.Println("AudioOut: Manual shutdown triggered")
			return
		}
	}
}

func (po *PortAudioOutput) callback(out []float32) {
	select {
	case data := <-po.audioChan:
		po.applyCompression(data, out)
	default:
		for i := range out {
			out[i] = 0 // Prevent audio glitches by sending silence
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
		close(po.shutdown)
		close(po.audioChan)
		if err := po.stream.Close(); err != nil {
			return err
		}
		po.stream = nil
	}
	return nil
}
