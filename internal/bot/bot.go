package bot

import (
        "log"
        "strings"

        "github.com/bwmarrin/discordgo"
        "github.com/viruslox/Ermete/internal/audio"
        "github.com/viruslox/Ermete/internal/config"
)

type Bot struct {
        session      *discordgo.Session
        ownerList    []string
        ShutdownChan chan struct{}
}

// Command Handlers
var commandHandlers = map[string]func(*discordgo.Session, *discordgo.MessageCreate, *Bot){
        "join":    handleJoinCommand,
        "leave":   handleLeaveCommand,
        "shutdown": handleShutdownCommand,
}

func NewBot() (*Bot, error) {
        dg, err := discordgo.New("Bot " + config.BotToken) // Access config values
        if err != nil {
                return nil, err
        }

        dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentGuildVoiceStates | discordgo.IntentsGuildMembers

        b := &Bot{
                session:      dg,
                ShutdownChan: make(chan struct{}),
        }

        dg.AddHandler(b.onReady)
        dg.AddHandler(b.onMessageCreate)
        dg.AddHandler(b.onVoiceStateUpdate)

        return b, nil
}

func (b *Bot) Open() error {
        return b.session.Open()
}

func (b *Bot) Close() {
        b.session.Close()
}

func (b *Bot) DisconnectAllVoiceConnections() {
        for _, vc := range b.session.VoiceConnections {
                if vc != nil {
                        if err := vc.Disconnect(); err != nil {
                                log.Printf("Error disconnecting from voice channel: %v", err)
                        }
                }
        }
}

func (b *Bot) onReady(s *discordgo.Session, event *discordgo.Ready) {
        log.Printf("Logged in as: %s#%s\n", event.User.Username, event.User.Discriminator)

        app, err := s.Application("@me")
        if err != nil {
                log.Printf("Error fetching bot application info: %v", err)
                return
        }

        log.Printf("Bot application info: %+v", app)

        if app.Team != nil {
                for _, member := range app.Team.Members {
                        b.ownerList = append(b.ownerList, member.User.ID)
                }
        } else {
                b.ownerList = append(b.ownerList, app.Owner.ID)
        }

        log.Printf("Owner(s) of the bot: %v", b.ownerList)
}

func (b *Bot) isOwner(userID string) bool {
        for _, ownerID := range b.ownerList {
                if ownerID == userID {
                        return true
                }
        }
        return false
}

func handleJoinCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot) {
        channelID := m.Content[len(config.CommandPrefix+"join "):]

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

        if err := audio.StartInput(vc, b.ShutdownChan); err != nil {
                log.Printf("Error starting input device: %v", err)
                return
        }
        if config.EnableOutput == "yes" { // Check the configuration flag
                if err := audio.StartOutput(vc, b.ShutdownChan); err != nil {
                        log.Printf("Error starting output device: %v", err)
                        return
                }
        } else {
                log.Println("Audio output is disabled.")
        }
}

func handleLeaveCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot) {
        vc, exists := s.VoiceConnections[m.GuildID]
        if !exists || vc == nil {
                log.Println("Bot is not connected to a voice channel.")
                return
        }

        log.Println("Stopping audio processing before leaving the voice channel.")

        if err := audio.StopInput(); err != nil {
                log.Printf("Error stopping input device: %v", err)
        }
        if config.EnableOutput == "yes" { // Check the configuration flag
        	if err := audio.StopOutput(); err != nil {
        	log.Printf("Error stopping output device: %v", err)
        	}
        } else {
        	log.Println("Audio output is disabled, skipping stop.")
        }

        log.Println("Bot is leaving the voice channel.")
        vc.Disconnect()
}

func handleShutdownCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot) {
        if !b.isOwner(m.Author.ID) {
                log.Println("Unauthorized shutdown attempt.")
                return
        }
        log.Println("Shutting down bot.")
        b.ShutdownChan <- struct{}{}
}

func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
        if m.Author.Bot {
                return
        }

        if !strings.HasPrefix(m.Content, config.CommandPrefix) {
                return
        }

        command := strings.TrimPrefix(m.Content, config.CommandPrefix)
        args := strings.Fields(command)
        if len(args) == 0 {
                return
        }

        if handler, exists := commandHandlers[args[0]]; exists {
                handler(s, m, b) // Pass the Bot instance here
        } else {
                log.Println("Unknown command:", args[0])
        }
}

func (b *Bot) onVoiceStateUpdate(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
        if v.UserID == s.State.User.ID {
                if v.ChannelID == "" {
                        log.Println("Bot left the voice channel.")
                } else {
                        log.Printf("Bot joined the voice channel: %s", v.ChannelID)
                }
        }
}
