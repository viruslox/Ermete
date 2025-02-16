package config

import (
        "log"
        "os"
)

var (
        CommandPrefix string
        BotToken      string
)

func LoadConfig() {
        CommandPrefix = os.Getenv("GOLIVE_BOT_PREFIX")
        if CommandPrefix == "" {
                CommandPrefix = "ermete." // Default value
        }

        BotToken = os.Getenv("GOLIVE_BOT_TOKEN")
        if BotToken == "" {
                log.Fatal("Bot token not set. Please set GOLIVE_BOT_TOKEN environment variable.")
        }
}
