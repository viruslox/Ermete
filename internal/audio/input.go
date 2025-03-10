package audio

import (
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gordonklaus/portaudio"
	"github.com/hraban/opus"
)

type PortAudioInput struct {
	stream      *portaudio.Stream
	vc          *discordgo.VoiceConnection
	opusEncoder *opus.Encoder
	audioChan   chan []float32
}

var inputDevice *PortAudioInput

func StartInput(vc *discordgo.VoiceConnection, shutdownChan <-chan struct{}) error {
	inputDevice = &PortAudioInput{}
	return inputDevice.startProcessing(vc, shutdownChan)
}

func StopInput() error {
	if inputDevice != nil {
		return inputDevice.Stop()
	}
	return nil
}

func (pi *PortAudioInput) startProcessing(vc *discordgo.VoiceConnection, shutdownChan <-chan struct{}) error {
	pi.vc = vc
	pi.audioChan = make(chan []float32, 80) // buffer size, till 500 works good

	var err error
	pi.opusEncoder, err = opus.NewEncoder(48000, 2, opus.AppAudio)
	if err != nil {
		return err
	}
	pi.opusEncoder.SetBitrate(96000)

	device, err := portaudio.DefaultInputDevice()
	if err != nil {
		return err
	}

	inputParams := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   device,
			Channels: 2, // Stereo input
		},
		SampleRate:    48000,
		FramesPerBuffer: 960,
	}

	pi.stream, err = portaudio.OpenStream(inputParams, pi.callback)
	if err != nil {
		return err
	}

	if err := pi.stream.Start(); err != nil {
		return err
	}

	go pi.processAudio(shutdownChan)

	return nil
}

func (pi *PortAudioInput) callback(in []float32, out []float32) {
	buffer := make([]float32, len(in))
	copy(buffer, in)

	select {
	case pi.audioChan <- buffer:
		// Successfully added to buffer
	case <-time.After(50 * time.Millisecond):
		log.Println("AudioIn Timeout while adding to audio buffer, dropping frame")
	}
}

func (pi *PortAudioInput) processAudio(shutdownChan <-chan struct{}) {
	defer log.Println("AudioIn Audio processing goroutine exited")
	encoded := make([]byte, 2048) // Reuse buffer

	for {
		select {
		case frame, ok := <-pi.audioChan:
			if !ok {
				log.Println("AudioIn Audio channel closed, stopping processing")
				return
			}

			n, err := pi.opusEncoder.EncodeFloat32(frame, encoded)
			if err != nil {
				log.Println("AudioIn Error encoding audio:", err)
				continue
			}

			select {
			case pi.vc.OpusSend <- encoded[:n]: // Send encoded data
				time.Sleep(5 * time.Millisecond) // Throttle sending
			case <-time.After(50 * time.Millisecond):
				log.Println("AudioIn Timeout while sending Opus data, dropping frame")
			}

		case <-shutdownChan:
			log.Println("AudioIn Shutting down audio input processing")
			return
		}
	}
}

func (pi *PortAudioInput) Stop() error {
	if pi.stream != nil {
		close(pi.audioChan) // Close the audio channel
		err := pi.stream.Close()
		if err != nil {
			return err
		}
		pi.stream = nil
	}
	return nil
}
