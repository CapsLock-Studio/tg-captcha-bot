package main

import (
	"log"
	"math/rand"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/viper"
	"golang.org/x/text/width"
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
var passedDialog = make(map[int]string)
var bot *tb.Bot

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

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

	rand.Seed(time.Now().Unix())
	questions := []int{rand.Intn(99), rand.Intn(99)}
	log.Printf("%v", questions)
	inlineKeys := [][]tb.InlineButton{}
	answer := rand.Intn(3)
	hashString := randStringBytes(10)
	for index := 0; index < 3; index++ {
		text := ""
		if index == answer {
			text = strconv.Itoa(questions[0] + questions[1])
		} else {
			text = strconv.Itoa(rand.Intn(99) + rand.Intn(99))
		}

		data := func(a int, b int) string {
			if a == b {
				return hashString
			}

			return randStringBytes(10)
		}

		inlineBtn := tb.InlineButton{
			Data: data(index, answer),
			Text: replaceFormula(text),
		}

		inlineKeys = append(inlineKeys, []tb.InlineButton{inlineBtn})
	}

	passedDialog[m.UserJoined.ID] = hashString
	welcomeMessage := config.WelcomeMessage
	welcomeMessage = strings.Replace(welcomeMessage, "{user}", m.UserJoined.FirstName+" "+m.UserJoined.LastName, -1)
	welcomeMessage = strings.Replace(welcomeMessage, "{formula}", replaceFormula(strconv.Itoa(questions[0])+"+"+strconv.Itoa(questions[1])), -1)

	challengeMsg, _ := bot.Reply(m, welcomeMessage, &tb.ReplyMarkup{InlineKeyboard: inlineKeys})

	time.AfterFunc(180*time.Second, func() {
		_, passed := passedUsers[m.UserJoined.ID]
		if !passed {
			chatMember := tb.ChatMember{User: m.UserJoined, RestrictedUntil: tb.Forever()}
			bot.Ban(m.Chat, &chatMember)

			if config.PrintSuccessAndFail == "show" {
				bot.Edit(challengeMsg, config.AfterFailMessage)

				time.AfterFunc(30*time.Second, func() {
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
		delete(passedDialog, m.UserJoined.ID)
	})
}

// passChallenge is used when user passed the validation
func passChallenge(c *tb.Callback) {
	if c.Message.ReplyTo.Sender.ID != c.Sender.ID {
		bot.Respond(c, &tb.CallbackResponse{Text: "This button isn't for you"})
		return
	}

	if c.Data != passedDialog[c.Sender.ID] {
		chatMember := tb.ChatMember{User: c.Message.ReplyTo.Sender, RestrictedUntil: tb.Forever()}
		bot.Edit(c.Message, config.AfterFailAnswerMessage)
		bot.Ban(c.Message.Chat, &chatMember)

		time.AfterFunc(30*time.Second, func() {
			bot.Delete(c.Message)
		})
		return
	}

	delete(passedUsers, c.Sender.ID)
	delete(passedDialog, c.Sender.ID)

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

func replaceFormula(str string) string {
	randString := func(strs []string) string {
		rand.Seed(time.Now().Unix())
		return strs[rand.Intn(len(strs))]
	}

	str = strings.Replace(str, "0", randString([]string{"O", "o"}), -1)
	str = strings.Replace(str, "1", randString([]string{"I", "l"}), -1)

	return width.Widen.String(str)
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
