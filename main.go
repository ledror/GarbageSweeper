package main

import (
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
)

const (
	tokenFileName = ".bottoken"
	owner         = "YOUR-DISCORD-ID"
)

var (
	token  string
	abused map[string]bool
)

func init() {
	currentUser, err := user.Current()
	if err != nil {
		fmt.Println("Error getting current user: ", err)
		os.Exit(1)
	}
	homeDir := currentUser.HomeDir
	tokenPath := filepath.Join(homeDir, tokenFileName)
	tokenContent, err := os.ReadFile(tokenPath)
	if err != nil {
		fmt.Printf("Error reading %s: %v\n", tokenPath, err)
		os.Exit(1)
	}
	token = string(tokenContent)
	token = strings.TrimSuffix(token, "\n")
	abused = make(map[string]bool)
}

func main() {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		fmt.Println("Error creating discord session: ", err)
		return
	}

	session.AddHandler(ready)
	session.AddHandler(voiceStateUpdate)
	session.AddHandler(messageCreate)

	session.Identify.Intents = discordgo.IntentsAll

	err = session.Open()
	if err != nil {
		fmt.Println("Error opening discord session: ", err)
		return
	}

	fmt.Println("Disconnector now running!")
	sc := make(chan os.Signal)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	session.Close()
}

func ready(s *discordgo.Session, event *discordgo.Event) {
	s.UpdateGameStatus(0, "Sweeping...")
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID || m.Author.ID != owner {
		return
	}

	// fmt.Println(m.Content)

	if strings.HasPrefix(m.Content, "!abuse") {
		addAbuse(s, m)
	} else if strings.HasPrefix(m.Content, "!pardon") {
		addPardon(s, m)
	}
}

func addAbuse(s *discordgo.Session, m *discordgo.MessageCreate) {
	statement := strings.Split(m.Content, " ")
	if len(statement) != 2 {
		return
	}
	target := strings.Trim(statement[1], " ")
	if !strings.HasPrefix(target, "<@") || !strings.HasSuffix(target, ">") {
		return
	}
	target = target[2 : len(target)-1]
	abused[target] = true
	err := s.GuildMemberMove(m.GuildID, target, nil)
	if err != nil {
		fmt.Printf("Couldn't disconnect user %s: %v\n", target, err)
		return
	}
	fmt.Println("Abusing", target)
}

func addPardon(s *discordgo.Session, m *discordgo.MessageCreate) {
	statement := strings.Split(m.Content, " ")
	if len(statement) != 2 {
		return
	}
	target := strings.Trim(statement[1], " ")
	if !strings.HasPrefix(target, "<@") || !strings.HasSuffix(target, ">") {
		return
	}
	target = target[2 : len(target)-1]
	delete(abused, target)
	fmt.Println("Pardoning", target)
}

func voiceStateUpdate(s *discordgo.Session, event *discordgo.VoiceStateUpdate) {
	_, ok := abused[event.UserID]
	if !ok {
		return
	}

	err := s.GuildMemberMove(event.GuildID, event.UserID, nil)
	if err != nil {
		fmt.Printf("Couldn't disconnect user %s: %v\n", event.UserID, err)
		return
	}
}
