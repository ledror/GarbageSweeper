package main

import (
	"cmp"
	"encoding/gob"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
)

const (
	tokenFileName  = ".bottoken"
	abusedGob      = "abused.gob"
	abusedCountGob = "abusedCount.gob"
	owner          = "YOUR-DISCORD-ID"
)

type GuildUser struct {
	guildID string
	userID  string
}

type (
	GuildMap       map[string]bool // set of users
	GuildCountMap  map[string]int
	AbusedMap      map[string]GuildMap // map from guildID to set of users
	AbusedCountMap map[string]GuildCountMap
)

var (
	token       string
	abused      AbusedMap
	abusedCount AbusedCountMap
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
	abused = loadAbusedMap()
	abusedCount = loadAbusedCountMap()
	fmt.Println(abused)
	fmt.Println(abusedCount)
}

func loadAbusedMap() AbusedMap {
	abuseMapFile, err := os.Open(abusedGob)
	if err != nil {
		return make(AbusedMap)
	}
	defer abuseMapFile.Close()
	decoder := gob.NewDecoder(abuseMapFile)
	m := make(AbusedMap)
	err = decoder.Decode(&m)
	if err != nil {
		return make(AbusedMap)
	}
	return m
}

func loadAbusedCountMap() AbusedCountMap {
	abuseCountMapFile, err := os.Open(abusedCountGob)
	if err != nil {
		return make(AbusedCountMap)
	}
	defer abuseCountMapFile.Close()
	decoder := gob.NewDecoder(abuseCountMapFile)
	m := make(AbusedCountMap)
	err = decoder.Decode(&m)
	if err != nil {
		return make(AbusedCountMap)
	}
	return m
}

func storeAbusedMap() error {
	abuseMapFile, err := os.OpenFile(abusedGob, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer abuseMapFile.Close()
	encoder := gob.NewEncoder(abuseMapFile)
	if err := encoder.Encode(abused); err != nil {
		return err
	}
	return nil
}

func storeAbusedCountMap() error {
	abusedCountMap, err := os.OpenFile(abusedCountGob, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer abusedCountMap.Close()
	encoder := gob.NewEncoder(abusedCountMap)
	if err := encoder.Encode(abusedCount); err != nil {
		return err
	}
	return nil
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
	if err := storeAbusedMap(); err != nil {
		fmt.Println("Error storing abused map", err)
	}
	if err := storeAbusedCountMap(); err != nil {
		fmt.Println("Error storing abused count map", err)
	}

	session.Close()
}

func ready(s *discordgo.Session, event *discordgo.Event) {
	s.UpdateGameStatus(0, "Sweeping...")
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	// fmt.Println(m.Content)

	if strings.HasPrefix(m.Content, "!abuse") {
		if m.Author.ID != owner {
			return
		}
		user := identifyUserInCommand(m)
		if user != "" {
			addAbuse(s, user, m.GuildID, m.ChannelID)
		}
	} else if strings.HasPrefix(m.Content, "!pardon") {
		if m.Author.ID != owner {
			return
		}
		user := identifyUserInCommand(m)
		if user != "" {
			addPardon(s, user, m.GuildID, m.ChannelID)
		}
	} else if strings.HasPrefix(m.Content, "!list") {
		listAbused(s, m.GuildID, m.ChannelID)
	} else if strings.HasPrefix(m.Content, "!count") {
		countAbused(s, m.GuildID, m.ChannelID)
	}
}

func identifyUserInCommand(m *discordgo.MessageCreate) (user string) {
	statement := strings.Split(m.Content, " ")
	if len(statement) != 2 {
		return
	}
	target := strings.Trim(statement[1], " ")
	if !strings.HasPrefix(target, "<@") || !strings.HasSuffix(target, ">") {
		return
	}
	user = target[2 : len(target)-1]
	return
}

func addAbuse(s *discordgo.Session, userID string, guildID string, channelID string) {
	if abused[guildID] == nil {
		abused[guildID] = make(GuildMap)
	}
	if _, ok := abused[guildID][userID]; ok {
		message := fmt.Sprintf("already abusing <@%s>, chillax", userID)
		s.ChannelMessageSend(channelID, message)
		fmt.Printf("Already abusing user %s on %s\n", userID, guildID)
		return
	}
	abused[guildID][userID] = true
	fmt.Printf("Abusing %s in %s\n", userID, guildID)
	message := fmt.Sprintf("abusing <@%s>, enjoy :)", userID)
	s.ChannelMessageSend(channelID, message)
	if isUserInVC(s, guildID, userID) {
		disconnectUser(s, guildID, userID)
	}
}

func addPardon(s *discordgo.Session, userID string, guildID string, channelID string) {
	notFound := false
	if abused[guildID] == nil {
		notFound = true
	} else if _, ok := abused[guildID][userID]; !ok {
		notFound = true
	}
	if notFound {
		message := fmt.Sprintf("<@%s> isn't being abused dummy", userID)
		s.ChannelMessageSend(channelID, message)
		fmt.Printf("%s wasn't abused\n", userID)
		return
	}
	delete(abused[guildID], userID)
	if len(abused[guildID]) == 0 {
		delete(abused, guildID)
	}
	message := fmt.Sprintf("pardoning <@%s>, stop being a jerk", userID)
	s.ChannelMessageSend(channelID, message)
	fmt.Printf("Pardoning %s in %s\n", userID, guildID)
}

func listAbused(s *discordgo.Session, guildID string, channelID string) {
	fmt.Println("Listing abused in guild", guildID)
	if abused[guildID] == nil || len(abused[guildID]) == 0 {
		message := fmt.Sprintf("no one is being abused")
		s.ChannelMessageSend(channelID, message)
		return
	}
	message := "currently abusing:"
	i := 1
	for user := range abused[guildID] {
		message += fmt.Sprintf("\n%d. <@%s>", i, user)
		i++
	}
	s.ChannelMessageSend(channelID, message)
}

type UserCount struct {
	user  string
	count int
}

func countAbused(s *discordgo.Session, guildID string, channelID string) {
	fmt.Println("counting abused in", guildID)
	if abusedCount[guildID] == nil || len(abusedCount[guildID]) == 0 {
		message := "no one was abused, stop crying"
		s.ChannelMessageSend(channelID, message)
		return
	}
	message := "abusement counts:"
	var userCounts []UserCount

	for user, count := range abusedCount[guildID] {
		userCounts = append(userCounts, UserCount{user, count})
	}

	slices.SortFunc(userCounts, func(a, b UserCount) int {
		return -cmp.Compare(a.count, b.count)
	})

	for rank, userCount := range userCounts {
		message += fmt.Sprintf("\n%d. <@%s> -- %d", rank, userCount.user, userCount.count)
	}
	s.ChannelMessageSend(channelID, message)
}

func isUserInVC(s *discordgo.Session, guildID string, userID string) bool {
	guild, err := s.State.Guild(guildID)
	if err != nil {
		fmt.Printf("Couldn't find guildID %s", guildID)
		return false
	}
	for _, key := range guild.VoiceStates {
		if userID == key.UserID {
			return true
		}
	}
	fmt.Printf("Couldn't find user %s in %s\n", userID, guildID)
	return false
}

func disconnectUser(s *discordgo.Session, guildID string, userID string) {
	err := s.GuildMemberMove(guildID, userID, nil)
	if err != nil {
		fmt.Printf("Couldn't disconnect user %s on %s: %v\n", userID, guildID, err)
		return
	}
	if abusedCount[guildID] == nil {
		abusedCount[guildID] = make(GuildCountMap)
	}
	abusedCount[guildID][userID]++
}

func voiceStateUpdate(s *discordgo.Session, event *discordgo.VoiceStateUpdate) {
	if event.BeforeUpdate != nil { // this isn't a "user connected" event
		return
	}
	if _, ok := abused[event.GuildID][event.UserID]; ok {
		if isUserInVC(s, event.GuildID, event.UserID) {
			disconnectUser(s, event.GuildID, event.UserID)
		}
	}
}
