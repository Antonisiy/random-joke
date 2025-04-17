package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/html/charset"
)

type Joke struct {
	Text      string `json:"joke"`
	Source    string `json:"source"`
	IsRussian bool   `json:"is_russian"`
}

// JokeProvider описывает интерфейс для получения анекдота
// Каждый провайдер реализует FetchJoke(ctx)
type JokeProvider interface {
	FetchJoke(ctx context.Context) (Joke, error)
}

// --- Реализации провайдеров ---

// DadJokeProvider для icanhazdadjoke.com
type DadJokeProvider struct{}

func (p DadJokeProvider) FetchJoke(ctx context.Context) (Joke, error) {
	url := "https://icanhazdadjoke.com"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return Joke{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MyJokeService (https://github.com/yourusername/joke-service)")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return Joke{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Joke{}, err
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" || !containsJSONContentType(ct) {
		return Joke{}, fmt.Errorf("ожидался JSON, но Content-Type: %s", ct)
	}
	var result struct {
		Joke string `json:"joke"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return Joke{}, err
	}
	return Joke{Text: result.Joke, Source: "icanhazdadjoke.com", IsRussian: false}, nil
}

// RzhunemoguProvider для rzhunemogu.ru
type RzhunemoguProvider struct{}

func (p RzhunemoguProvider) FetchJoke(ctx context.Context) (Joke, error) {
	url := "http://rzhunemogu.ru/RandJSON.aspx?CType=1"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return Joke{}, err
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return Joke{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Joke{}, err
	}
	body, err = decodeWindows1251(body)
	if err != nil {
		return Joke{}, err
	}
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	body = bytes.TrimSpace(body)
	body = bytes.ReplaceAll(body, []byte("\r\n"), []byte("\\r\\n"))
	var result struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return Joke{}, err
	}
	return Joke{Text: result.Content, Source: "rzhunemogu.ru", IsRussian: true}, nil
}

// JokeAPIProvider для jokeapi.dev
type JokeAPIProvider struct{}

func (p JokeAPIProvider) FetchJoke(ctx context.Context) (Joke, error) {
	url := "https://v2.jokeapi.dev/joke/Any?type=single"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return Joke{}, err
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return Joke{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Joke{}, err
	}
	var result struct {
		Joke     string `json:"joke"`
		Type     string `json:"type"`
		Error    bool   `json:"error"`
		Message  string `json:"message"`
		Setup    string `json:"setup"`
		Delivery string `json:"delivery"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return Joke{}, err
	}
	if result.Error {
		return Joke{}, fmt.Errorf("JokeAPI error: %s", result.Message)
	}
	if result.Joke != "" {
		return Joke{Text: result.Joke, Source: "jokeapi.dev", IsRussian: false}, nil
	} else if result.Setup != "" && result.Delivery != "" {
		return Joke{Text: result.Setup + "\n" + result.Delivery, Source: "jokeapi.dev", IsRussian: false}, nil
	}
	return Joke{}, fmt.Errorf("JokeAPI: пустой анекдот")
}

var (
	logger        = logrus.New()
	jokeProviders = []JokeProvider{
		DadJokeProvider{},
		RzhunemoguProvider{},
		JokeAPIProvider{},
	}
	port       = flag.String("port", "8888", "Порт для запуска сервера")
	jokeMemory map[int64]string
)

func main() {
	flag.Parse()

	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetOutput(os.Stdout)

	router := mux.NewRouter()
	// Подключаем middleware
	router.Use(loggingMiddleware)
	router.Use(corsMiddleware) // Добавляем CORS middleware
	router.HandleFunc("/random-joke", getRandomJoke).Methods("GET")
	router.HandleFunc("/translate", translateHandler).Methods("POST")
	// Новый endpoint для Telegram webhook
	router.HandleFunc("/telegram-webhook", telegramWebhookHandler).Methods("POST")
	// Кастомный обработчик статики для SPA
	router.PathPrefix("/").HandlerFunc(spaHandler)

	logger.Infof("Сервис запущен на порту :%s", *port)
	log.Fatal(http.ListenAndServe(":"+*port, router))
}

// spaHandler отдаёт index.html для GET-запросов к несуществующим файлам (SPA-режим)
func spaHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		// Для не-GET запросов возвращаем 404
		http.NotFound(w, r)
		return
	}
	path := "static" + r.URL.Path
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	// Если файла нет, отдаём index.html
	http.ServeFile(w, r, "static/index.html")
}

func getRandomJoke(w http.ResponseWriter, r *http.Request) {
	rand.Seed(time.Now().UnixNano())
	provider := jokeProviders[rand.Intn(len(jokeProviders))]
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	joke, err := provider.FetchJoke(ctx)
	if err != nil {
		http.Error(w, "Анекдоты временно недоступны", http.StatusInternalServerError)
		logger.Errorf("Ошибка получения анекдота: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(joke)
}

// containsJSONContentType проверяет, содержит ли Content-Type подстроку "application/json"
func containsJSONContentType(ct string) bool {
	return len(ct) >= 16 && (ct == "application/json" || (len(ct) > 16 && ct[:16] == "application/json")) || (len(ct) > 0 && (ct == "application/json; charset=utf-8" || ct == "application/json; charset=UTF-8")) || (len(ct) > 0 && (ct == "application/json; charset=windows-1251")) || (len(ct) > 0 && (ct == "application/json; charset=cp1251")) || (len(ct) > 0 && (ct == "application/json; charset=iso-8859-1")) || (len(ct) > 0 && (ct == "application/json; charset=ISO-8859-1")) || (len(ct) > 0 && (ct == "application/json; charset=us-ascii"))
}

// Декодирует Windows-1251 → UTF-8
func decodeWindows1251(body []byte) ([]byte, error) {
	reader, err := charset.NewReaderLabel("windows-1251", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(reader)
}

// translateHandler переводит текст анекдота на русский язык через Google Translate proxy
func translateHandler(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		Text string `json:"text"`
	}
	type respBody struct {
		Translation string `json:"translation"`
	}
	var body reqBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Text == "" {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	// Формируем URL для Google Translate proxy
	url := "https://translate.googleapis.com/translate_a/single?client=gtx&sl=en&tl=ru&dt=t&q=" + urlQueryEscape(body.Text)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		logger.Errorf("Ошибка создания запроса к Google Translate: %v", err)
		http.Error(w, "Ошибка перевода", http.StatusInternalServerError)
		return
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logger.Errorf("Ошибка обращения к Google Translate: %v", err)
		http.Error(w, "Ошибка перевода", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	var data interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		logger.Errorf("Ошибка декодирования ответа Google Translate: %v", err)
		http.Error(w, "Ошибка перевода", http.StatusInternalServerError)
		return
	}
	// Извлекаем перевод из data
	translation := ""
	if arr, ok := data.([]interface{}); ok && len(arr) > 0 {
		if innerArr, ok := arr[0].([]interface{}); ok {
			for _, seg := range innerArr {
				if segArr, ok := seg.([]interface{}); ok && len(segArr) > 0 {
					if str, ok := segArr[0].(string); ok {
						translation += str
					}
				}
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(respBody{Translation: translation})
}

// urlQueryEscape экранирует строку для URL-параметра
func urlQueryEscape(s string) string {
	return (&url.URL{Path: s}).EscapedPath()[1:]
}

// fetchRandomJoke возвращает случайный анекдот (используется ботом)
func fetchRandomJoke() (Joke, error) {
	rand.Seed(time.Now().UnixNano())
	provider := jokeProviders[rand.Intn(len(jokeProviders))]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return provider.FetchJoke(ctx)
}

// fetchRzhunemoguJoke возвращает анекдот с rzhunemogu.ru (используется ботом)
func fetchRzhunemoguJoke() (Joke, error) {
	provider := RzhunemoguProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return provider.FetchJoke(ctx)
}

// translateText переводит текст анекдота на русский язык через Google Translate proxy
func translateText(text string) (string, error) {
	url := "https://translate.googleapis.com/translate_a/single?client=gtx&sl=en&tl=ru&dt=t&q=" + urlQueryEscape(text)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var data interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	translation := ""
	if arr, ok := data.([]interface{}); ok && len(arr) > 0 {
		if innerArr, ok := arr[0].([]interface{}); ok {
			for _, seg := range innerArr {
				if segArr, ok := seg.([]interface{}); ok && len(segArr) > 0 {
					if str, ok := segArr[0].(string); ok {
						translation += str
					}
				}
			}
		}
	}
	return translation, nil
}

// telegramWebhookHandler обрабатывает входящие webhook-запросы Telegram
func telegramWebhookHandler(w http.ResponseWriter, r *http.Request) {
	bot, err := tgbotapi.NewBotAPI("7868740739:AAEkNOti1b1yGF04N12WNQ1uJJSAupdwh8U")
	if err != nil {
		logger.Errorf("Ошибка запуска Telegram-бота: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	update := tgbotapi.Update{}
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		logger.Errorf("Ошибка декодирования webhook: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	processTelegramUpdate(bot, update)
	w.WriteHeader(http.StatusOK)
}

// processTelegramUpdate обрабатывает update (логика Telegram-бота)
func processTelegramUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.CallbackQuery != nil && update.CallbackQuery.Data == "translate_joke" {
		chatID := update.CallbackQuery.Message.Chat.ID
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "Переведено")
		bot.Request(callback)
		edit := tgbotapi.NewEditMessageReplyMarkup(chatID, update.CallbackQuery.Message.MessageID, tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}})
		bot.Send(edit)
		if jokeMemory != nil {
			if jokeText, ok := jokeMemory[chatID]; ok {
				translation, err := translateText(jokeText)
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
	if update.Message == nil {
		return
	}
	if update.Message.IsCommand() {
		switch update.Message.Command() {
		case "start":
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Привет! Я бот-анекдотчик 🤖\n\nЯ умею присылать случайные анекдоты из разных источников. Просто отправь команду /joke, чтобы получить свежий анекдот!\n\nТакже я могу переводить анекдоты на русский язык, если потребуется.\n\nПиши /joke — и улыбка гарантирована!")
			bot.Send(msg)
		case "joke":
			joke, err := fetchRandomJoke()
			if err != nil {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Анекдоты временно недоступны")
				bot.Send(msg)
				return
			}
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, joke.Text)
			if !joke.IsRussian {
				keyboard := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Перевести на русский", "translate_joke"),
					),
				)
				msg.ReplyMarkup = keyboard
				if jokeMemory == nil {
					jokeMemory = make(map[int64]string)
				}
				jokeMemory[update.Message.Chat.ID] = joke.Text
			}
			bot.Send(msg)
		case "joke_ru":
			joke, err := fetchRzhunemoguJoke()
			if err != nil {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Русские анекдоты временно недоступны")
				bot.Send(msg)
				return
			}
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, joke.Text)
			bot.Send(msg)
		default:
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Используйте /joke для получения случайного анекдота.")
			bot.Send(msg)
		}
	}
}

// loggingMiddleware логирует все HTTP-запросы
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{w, http.StatusOK}
		next.ServeHTTP(rw, r)
		logger.Infof("[HTTP] %s %s %d %s", r.Method, r.URL.Path, rw.statusCode, time.Since(start))
	})
}

// corsMiddleware добавляет CORS заголовки
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowedOrigins := []string{
			"http://localhost:5173",
			"http://localhost",
			"https://welcome-cattle-regular.ngrok-free.app",
		}
		origin := r.Header.Get("Origin")
		for _, allowed := range allowedOrigins {
			if origin == allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				break
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
