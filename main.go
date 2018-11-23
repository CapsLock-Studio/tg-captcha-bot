package main

import (
	"log"
	"math/rand"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/viper"
	tb "gopkg.in/tucnak/telebot.v2"
)

// Config struct for toml config file
type Config struct {
	WelcomeMessage         string `mapstructure:"welcome_message"`
	AfterSuccessMessage    string `mapstructure:"after_success_message"`
	AfterFailMessage       string `mapstructure:"after_fail_message"`
	AfterFailAnswerMessage string `mapstructure:"after_fail_answer_message"`
	PrintSuccessAndFail    string `mapstructure:"print_success_and_fail_messages_strategy"`
}

var config Config
var passedUsers = make(map[int]struct{})
var bot *tb.Bot

func init() {
	err := readConfig()
	if err != nil {
		log.Fatalf("Cannot read config file. Error: %v", err)
	}
}

func main() {
	token, e := getToken()
	if e != nil {
		log.Fatalln(e)
	}
	log.Printf("Telegram Bot Token [%v] successfully obtained from env variable $TGTOKEN\n", token)

	var err error
	bot, err = tb.NewBot(tb.Settings{
		Token:  token,
		Poller: &tb.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatalf("Cannot start bot. Error: %v\n", err)
	}

	bot.Handle(tb.OnUserJoined, challengeUser)
	bot.Handle(tb.OnCallback, passChallenge)

	bot.Handle("/healthz", func(m *tb.Message) {
		msg := "I'm OK"
		if _, err := bot.Send(m.Chat, msg); err != nil {
			log.Println(err)
		}
		log.Printf("Healthz request from user: %v\n in chat: %v", m.Sender, m.Chat)
	})

	log.Println("Bot started!")
	go func() {
		bot.Start()
	}()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan
	log.Println("Shutdown signal received, exiting...")
}

func challengeUser(m *tb.Message) {
	if m.UserJoined.ID != m.Sender.ID {
		return
	}

	log.Printf("User: %v joined the chat: %v", m.UserJoined, m.Chat)
	newChatMember := tb.ChatMember{User: m.UserJoined, RestrictedUntil: tb.Forever(), Rights: tb.Rights{CanSendMessages: false}}
	bot.Restrict(m.Chat, &newChatMember)

	questions := []int{rand.Intn(99), rand.Intn(99)}
	log.Printf("%v", questions)
	inlineKeys := [][]tb.InlineButton{}
	answer := rand.Intn(3)
	for index := 0; index < 3; index++ {
		text := ""
		if index == answer {
			text = string(questions[0] + questions[1])
		} else {
			text = string(rand.Intn(99) + rand.Intn(99))
		}

		data := func(a int, b int) string {
			if a == b {
				return "Yes"
			}

			return "No"
		}

		inlineBtn := tb.InlineButton{
			Data: data(index, answer),
			Text: text,
		}

		inlineKeys = append(inlineKeys, []tb.InlineButton{inlineBtn})
	}

	welcomeMessage := config.WelcomeMessage
	welcomeMessage = strings.Replace(welcomeMessage, "{user}", m.UserJoined.FirstName+" "+m.UserJoined.LastName, -1)
	welcomeMessage = strings.Replace(welcomeMessage, "{formula}", string(questions[0])+"+"+string(questions[1]), -1)

	challengeMsg, _ := bot.Reply(m, welcomeMessage, &tb.ReplyMarkup{InlineKeyboard: inlineKeys})

	time.AfterFunc(180*time.Second, func() {
		_, passed := passedUsers[m.UserJoined.ID]
		if !passed {
			chatMember := tb.ChatMember{User: m.UserJoined, RestrictedUntil: tb.Forever()}
			bot.Ban(m.Chat, &chatMember)

			if config.PrintSuccessAndFail == "show" {
				bot.Edit(challengeMsg, config.AfterFailMessage)

				time.AfterFunc(10*time.Second, func() {
					bot.Delete(m)
					bot.Delete(challengeMsg)
				})
			} else if config.PrintSuccessAndFail == "delete" {
				bot.Delete(m)
				bot.Delete(challengeMsg)
			}

			log.Printf("User: %v was banned in chat: %v", m.UserJoined, m.Chat)
		}

		delete(passedUsers, m.UserJoined.ID)
	})
}

// passChallenge is used when user passed the validation
func passChallenge(c *tb.Callback) {
	if c.Message.ReplyTo.Sender.ID != c.Sender.ID {
		bot.Respond(c, &tb.CallbackResponse{Text: "This button isn't for you"})
		return
	}

	if c.Data == "No" {
		chatMember := tb.ChatMember{User: c.Message.ReplyTo.Sender, RestrictedUntil: tb.Forever()}
		bot.Edit(c.Message, config.AfterFailAnswerMessage)
		bot.Ban(c.Message.Chat, &chatMember)

		return
	}

	passedUsers[c.Sender.ID] = struct{}{}

	if config.PrintSuccessAndFail == "show" {
		bot.Edit(c.Message, config.AfterSuccessMessage)
	} else if config.PrintSuccessAndFail == "delete" {
		bot.Delete(c.Message)
	}

	log.Printf("User: %v passed the challenge in chat: %v", c.Sender, c.Message.Chat)
	newChatMember := tb.ChatMember{User: c.Sender, RestrictedUntil: tb.Forever(), Rights: tb.Rights{CanSendMessages: true}}
	bot.Promote(c.Message.Chat, &newChatMember)
	bot.Respond(c, &tb.CallbackResponse{Text: "Validation passed!"})
}

func readConfig() (err error) {
	v := viper.New()
	v.SetConfigName("config")
	v.AddConfigPath(".")

	if err = v.ReadInConfig(); err != nil {
		return err
	}
	if err = v.Unmarshal(&config); err != nil {
		return err
	}
	return
}

func getToken() (string, error) {
	token, ok := os.LookupEnv("TGTOKEN")
	if !ok {
		err := errors.Errorf("Env variable TGTOKEN isn't set!")
		return "", err
	}

	match, err := regexp.MatchString(`^[0-9]+:.*$`, token)
	if err != nil {
		return "", err
	}

	if !match {
		err := errors.Errorf("Telegram Bot Token [%v] is incorrect. Token doesn't comply with regexp: `^[0-9]+:.*$`. Please, provide a correct Telegram Bot Token through env variable TGTOKEN", token)
		return "", err
	}

	return token, nil
}

func getEnv(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}

	return fallback
}
