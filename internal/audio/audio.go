package audio

import (
        "log"

        "github.com/gordonklaus/portaudio"
)

func Initialize() error {
        err := portaudio.Initialize()
        if err != nil {
                return err
        }

        devices, err := portaudio.Devices()
        if err != nil {
                log.Fatal("Error listing PortAudio devices:", err)
        }
        for i, device := range devices {
                log.Printf("Device %d: Name: %s, Input Channels: %d, Output Channels: %d", i, device.Name, device.MaxInputChannels, device.MaxOutputChannels)
        }
        return nil
}

func Terminate() error {
    return portaudio.Terminate()
}
