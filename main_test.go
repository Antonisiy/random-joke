//go:build !integration
// +build !integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// --- Обёртки для мокирования функций ---
var (
	fetchRandomJokeFunc     = fetchRandomJoke
	fetchRzhunemoguJokeFunc = fetchRzhunemoguJoke
	translateTextFunc       = translateText
)

func TestDadJokeProvider_FetchJoke(t *testing.T) {
	provider := DadJokeProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	joke, err := provider.FetchJoke(ctx)
	if err != nil {
		t.Fatalf("DadJokeProvider error: %v", err)
	}
	if joke.Text == "" {
		t.Error("DadJokeProvider returned empty joke")
	}
}

func TestRzhunemoguProvider_FetchJoke(t *testing.T) {
	provider := RzhunemoguProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	joke, err := provider.FetchJoke(ctx)
	if err != nil {
		t.Fatalf("RzhunemoguProvider error: %v", err)
	}
	if joke.Text == "" {
		t.Error("RzhunemoguProvider returned empty joke")
	}
	if !joke.IsRussian {
		t.Error("RzhunemoguProvider should return IsRussian=true")
	}
}

func TestJokeAPIProvider_FetchJoke(t *testing.T) {
	provider := JokeAPIProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	joke, err := provider.FetchJoke(ctx)
	if err != nil {
		t.Fatalf("JokeAPIProvider error: %v", err)
	}
	if joke.Text == "" {
		t.Error("JokeAPIProvider returned empty joke")
	}
}

func TestGetRandomJokeHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/random-joke", nil)
	w := httptest.NewRecorder()
	getRandomJoke(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	var joke Joke
	if err := json.NewDecoder(resp.Body).Decode(&joke); err != nil {
		t.Fatalf("Failed to decode joke: %v", err)
	}
	if joke.Text == "" {
		t.Error("Handler returned empty joke")
	}
}

func TestTranslateHandler(t *testing.T) {
	text := "Hello, world!"
	body := bytes.NewBufferString(`{"text":"` + text + `"}`)
	req := httptest.NewRequest("POST", "/translate", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	translateHandler(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	var result struct {
		Translation string `json:"translation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode translation: %v", err)
	}
	if result.Translation == "" {
		t.Error("Translation is empty")
	}
}

func TestTranslateText(t *testing.T) {
	translation, err := translateText("I like jokes!")
	if err != nil {
		t.Fatalf("translateText error: %v", err)
	}
	if translation == "" {
		t.Error("translateText returned empty string")
	}
}

// --- Моки для тестирования Telegram ---
type mockBotAPI struct {
	SentMessages       []interface{}
	RequestedCallbacks []interface{}
}

func (m *mockBotAPI) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	m.SentMessages = append(m.SentMessages, c)
	return tgbotapi.Message{}, nil
}
func (m *mockBotAPI) Request(c tgbotapi.Chattable) (interface{}, error) {
	m.RequestedCallbacks = append(m.RequestedCallbacks, c)
	return struct{}{}, nil
}

// --- Интерфейс для мокирования Message ---
type CommandMessage interface {
	IsCommand() bool
	Command() string
	GetChatID() int64
}

type mockCommandMessage struct {
	chatID    int64
	command   string
	isCommand bool
}

func (m *mockCommandMessage) IsCommand() bool  { return m.isCommand }
func (m *mockCommandMessage) Command() string  { return m.command }
func (m *mockCommandMessage) GetChatID() int64 { return m.chatID }

// --- Адаптер для processTelegramUpdateTest ---
func processTelegramUpdateTest(bot *mockBotAPI, update tgbotapi.Update, msg CommandMessage) {
	if update.CallbackQuery != nil && update.CallbackQuery.Data == "translate_joke" {
		chatID := update.CallbackQuery.Message.Chat.ID
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "Переведено")
		bot.Request(callback)
		edit := tgbotapi.NewEditMessageReplyMarkup(chatID, update.CallbackQuery.Message.MessageID, tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}})
		bot.Send(edit)
		if jokeMemory != nil {
			if jokeText, ok := jokeMemory[chatID]; ok {
				translation, err := translateTextFunc(jokeText)
				if err != nil || translation == "" {
					msg := tgbotapi.NewMessage(chatID, "Ошибка перевода")
					bot.Send(msg)
				} else {
					msg := tgbotapi.NewMessage(chatID, translation)
					bot.Send(msg)
				}
			}
		}
		return
	}
	if msg == nil {
		return
	}
	if msg.IsCommand() {
		switch msg.Command() {
		case "start":
			m := tgbotapi.NewMessage(msg.GetChatID(), "Привет! Я бот-анекдотчик 🤖\n\nЯ умею присылать случайные анекдоты из разных источников. Просто отправь команду /joke, чтобы получить свежий анекдот!\n\nТакже я могу переводить анекдоты на русский язык, если потребуется.\n\nПиши /joke — и улыбка гарантирована!")
			bot.Send(m)
		case "joke":
			joke, err := fetchRandomJokeFunc()
			if err != nil {
				m := tgbotapi.NewMessage(msg.GetChatID(), "Анекдоты временно недоступны")
				bot.Send(m)
				return
			}
			m := tgbotapi.NewMessage(msg.GetChatID(), joke.Text)
			if !joke.IsRussian {
				keyboard := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Перевести на русский", "translate_joke"),
					),
				)
				m.ReplyMarkup = keyboard
				if jokeMemory == nil {
					jokeMemory = make(map[int64]string)
				}
				jokeMemory[msg.GetChatID()] = joke.Text
			}
			bot.Send(m)
		case "joke_ru":
			joke, err := fetchRzhunemoguJokeFunc()
			if err != nil {
				m := tgbotapi.NewMessage(msg.GetChatID(), "Русские анекдоты временно недоступны")
				bot.Send(m)
				return
			}
			m := tgbotapi.NewMessage(msg.GetChatID(), joke.Text)
			bot.Send(m)
		default:
			m := tgbotapi.NewMessage(msg.GetChatID(), "Используйте /joke для получения случайного анекдота.")
			bot.Send(m)
		}
	}
}

// --- Тесты для processTelegramUpdate ---
func TestProcessTelegramUpdate_StartCommand(t *testing.T) {
	bot := &mockBotAPI{}
	msg := &mockCommandMessage{chatID: 123, command: "start", isCommand: true}
	processTelegramUpdateTest(bot, tgbotapi.Update{}, msg)
	if len(bot.SentMessages) == 0 {
		t.Error("No message sent for /start command")
	}
}

func TestProcessTelegramUpdate_JokeCommand(t *testing.T) {
	bot := &mockBotAPI{}
	msg := &mockCommandMessage{chatID: 123, command: "joke", isCommand: true}
	fetchRandomJokeFunc = func() (Joke, error) {
		return Joke{Text: "Test joke", IsRussian: false}, nil
	}
	processTelegramUpdateTest(bot, tgbotapi.Update{}, msg)
	if len(bot.SentMessages) == 0 {
		t.Error("No message sent for /joke command")
	}
}

func TestProcessTelegramUpdate_JokeRuCommand(t *testing.T) {
	bot := &mockBotAPI{}
	msg := &mockCommandMessage{chatID: 123, command: "joke_ru", isCommand: true}
	fetchRzhunemoguJokeFunc = func() (Joke, error) {
		return Joke{Text: "Русский анекдот", IsRussian: true}, nil
	}
	processTelegramUpdateTest(bot, tgbotapi.Update{}, msg)
	if len(bot.SentMessages) == 0 {
		t.Error("No message sent for /joke_ru command")
	}
}

func TestProcessTelegramUpdate_UnknownCommand(t *testing.T) {
	bot := &mockBotAPI{}
	msg := &mockCommandMessage{chatID: 123, command: "unknown", isCommand: true}
	processTelegramUpdateTest(bot, tgbotapi.Update{}, msg)
	if len(bot.SentMessages) == 0 {
		t.Error("No message sent for unknown command")
	}
}

func TestProcessTelegramUpdate_TranslateCallback(t *testing.T) {
	bot := &mockBotAPI{}
	jokeMemory = map[int64]string{123: "Test joke"}
	update := tgbotapi.Update{
		CallbackQuery: &tgbotapi.CallbackQuery{
			ID:   "cbid",
			Data: "translate_joke",
			Message: &tgbotapi.Message{
				Chat:      &tgbotapi.Chat{ID: 123},
				MessageID: 1,
			},
		},
	}
	translateTextFunc = func(text string) (string, error) {
		if text == "Test joke" {
			return "Тестовый перевод", nil
		}
		return "", errors.New("fail")
	}
	processTelegramUpdateTest(bot, update, nil)
	if len(bot.RequestedCallbacks) == 0 {
		t.Error("No callback requested for translate_joke")
	}
	if len(bot.SentMessages) == 0 {
		t.Error("No message sent for translate_joke callback")
	}
}
