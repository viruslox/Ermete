package main

import (
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/viruslox/Ermete/internal/audio"
    "github.com/viruslox/Ermete/internal/bot"
    "github.com/viruslox/Ermete/internal/config"
)

func main() {
    config.LoadConfig() // Load configuration (env vars, etc.)

    err := audio.Initialize()
    if err != nil {
        log.Fatalf("Audio initialization failed: %v", err)
    }
    defer audio.Terminate()

    b, err := bot.NewBot()
    if err != nil {
        log.Fatalf("Error creating bot: %v", err)
    }

    err = b.Open()
    if err != nil {
        log.Fatalf("Error opening bot: %v", err)
    }
    defer b.Close()

    log.Println("Bot is now running. Press CTRL+C to exit.")

    shutdown := make(chan os.Signal, 1)
    signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

    select {
    case <-shutdown:
        log.Println("Received OS shutdown signal (Ctrl+C).")
    case <-b.ShutdownChan:
        log.Println("Received Discord shutdown command.")
    }

    log.Println("Starting bot shutdown...")

    timeout := time.After(5 * time.Second)
    stopErr := make(chan error, 2)

    go func() {
        if err := audio.StopInput(); err != nil {
            stopErr <- err
        } else {
            stopErr <- nil
        }
    }()
    go func() {
        if err := audio.StopOutput(); err != nil {
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

    b.DisconnectAllVoiceConnections()

    time.Sleep(2 * time.Second)
    log.Println("Bot shutdown complete.")
}
