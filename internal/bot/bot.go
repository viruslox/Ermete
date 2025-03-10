package bot

import (
	"fmt"
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
	voiceUsers     map[string]uint32 // Map userID -> SSRC
	voiceChannelID string            // Track the voice channel bot is in
	commandChannel string            // The text channel where the join command was sent
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
		voiceUsers:   make(map[string]uint32),
        }

        dg.AddHandler(b.onReady)
        dg.AddHandler(b.onMessageCreate)
        dg.AddHandler(b.onVoiceStateUpdate)
	dg.AddHandler(b.onVoiceSpeakingUpdate)

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

func (b *Bot) isOwner(s *discordgo.Session, guildID, userID string) bool {
    guild, err := s.Guild(guildID)
    if err != nil {
        log.Printf("Error fetching guild: %v", err)
        return false
    }
    return guild.OwnerID == userID
}

func handleJoinCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot) {
	channelID := m.Content[len(config.CommandPrefix+"join "):] // Extract channel ID from message
	b.commandChannel = m.ChannelID // Save the text channel ID

    vc, exists := s.VoiceConnections[m.GuildID]
    if exists && vc.ChannelID == channelID {
        log.Println("Bot is already in the voice channel.")
        s.ChannelMessageSend(m.ChannelID, "I'm already in that voice channel.")
        return
    }

    log.Printf("Attempting to join channel: %s", channelID)

    vc, err := s.ChannelVoiceJoin(m.GuildID, channelID, false, false)
    if err != nil {
        log.Printf("Error joining voice channel: %v", err)
        s.ChannelMessageSend(m.ChannelID, "Failed to join the voice channel.")
        return
    }

    b.voiceChannelID = channelID
    log.Printf("Bot voice channel ID set to: %s", b.voiceChannelID) // Add this line
    s.ChannelMessageSend(m.ChannelID, "Joined the voice channel successfully!")

    if err := audio.StartInput(vc, b.ShutdownChan); err != nil {
        log.Printf("Error starting input device: %v", err)
        s.ChannelMessageSend(m.ChannelID, "Error starting audio input.")
        return
    }
    if config.EnableOutput == "yes" {
        if err := audio.StartOutput(vc, b.ShutdownChan); err != nil {
            log.Printf("Error starting output device: %v", err)
            s.ChannelMessageSend(m.ChannelID, "Error starting audio output.")
            return
        }
    } else {
        s.ChannelMessageSend(m.ChannelID, "Audio output is disabled.")
    }
}

func handleLeaveCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot) {
    vc, exists := s.VoiceConnections[m.GuildID]
    if !exists || vc == nil {
        log.Println("Bot is not connected to a voice channel.")
        s.ChannelMessageSend(m.ChannelID, "I'm not in any voice channel.")
        return
    }

    log.Println("Stopping audio processing before leaving the voice channel.")

    if err := audio.StopInput(); err != nil {
        log.Printf("Error stopping input device: %v", err)
        s.ChannelMessageSend(m.ChannelID, "Error stopping audio input.")
    }
    if config.EnableOutput == "yes" {
        if err := audio.StopOutput(); err != nil {
            log.Printf("Error stopping output device: %v", err)
            s.ChannelMessageSend(m.ChannelID, "Error stopping audio output.")
        }
    } else {
        log.Println("Audio output is disabled, skipping stop.")
    }

    log.Println("Bot is leaving the voice channel.")
    vc.Disconnect()
    s.ChannelMessageSend(m.ChannelID, "Left the voice channel.")
}

func handleShutdownCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot) {
    if !b.isOwner(s, m.GuildID, m.Author.ID) { // Corrected line
        log.Println("Unauthorized shutdown attempt.")
        s.ChannelMessageSend(m.ChannelID, "You are not authorized to shut me down.")
        return
    }
    log.Println("Shutting down bot.")
    s.ChannelMessageSend(m.ChannelID, "Shutting down...")
    close(b.ShutdownChan)
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
        // Check if the user is the server owner before executing the command.
        if b.isOwner(s, m.GuildID, m.Author.ID) { // Corrected line
            handler(s, m, b)
        } else {
            s.ChannelMessageSend(m.ChannelID, "Only the server owner can use this command.")
        }
    } else {
        log.Println("Unknown command:", args[0])
    }
}

func (b *Bot) onVoiceStateUpdate(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
    log.Printf("Voice state update: UserID=%s, ChannelID=%s, BotChannelID=%s", v.UserID, v.ChannelID, b.voiceChannelID)

    if v.UserID == s.State.User.ID {
        if v.ChannelID == "" {
            log.Println("Bot left the voice channel.")
        } else {
            log.Printf("Bot joined the voice channel: %s", v.ChannelID)
        }
    }

    if v.ChannelID == b.voiceChannelID {
        log.Printf("User joined: %s", v.UserID)
        // TEST: Add user to the map, need to get the SSRC from speaking events - with a default ssrc value of 0.
        b.voiceUsers[v.UserID] = 0
        b.updateVoiceUserList(s)
    } else if v.ChannelID == "" {
        log.Printf("User left: %s", v.UserID)
        delete(b.voiceUsers, v.UserID)
        b.updateVoiceUserList(s)
    }
}

func (b *Bot) onVoiceSpeakingUpdate(s *discordgo.Session, su *discordgo.VoiceSpeakingUpdate) {
    log.Printf("Voice speaking update: UserID=%s, SSRC=%d", su.UserID, su.SSRC)
    b.voiceUsers[su.UserID] = uint32(su.SSRC) // Store the SSRC
    log.Printf("User %s is speaking with SSRC %d", su.UserID, su.SSRC)
    b.updateVoiceUserList(s)
}

func (b *Bot) updateVoiceUserList(s *discordgo.Session) {
    log.Printf("Updating voice user list. Current users: %v", b.voiceUsers)
    if b.commandChannel == "" {
        return
    }

    message := "**Users in Voice Channel:**\n"
    if len(b.voiceUsers) == 0 {
        message += "_No users currently in voice._"
    } else {
        for userID, ssrc := range b.voiceUsers {
            message += fmt.Sprintf("- <@%s> (SSRC: %d)\n", userID, ssrc)
        }
    }

    log.Println(message)
    s.ChannelMessageSend(b.commandChannel, message)
}
