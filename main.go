package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/html/charset"
)

var (
	port = flag.String("port", "8888", "Порт для запуска сервера")
)

func main() {
	flag.Parse()

	// Set up logging
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
		DisableColors: false,
	})
	logger.SetLevel(logrus.DebugLevel)
	logger.SetOutput(os.Stderr)

	router := mux.NewRouter()
	// Подключаем middleware
	router.Use(loggingMiddleware)
	router.Use(corsMiddleware)

	// Регистрируем маршруты
	router.HandleFunc("/random-joke", getRandomJoke).Methods("GET")
	router.HandleFunc("/translate", translateHandler).Methods("POST")
	router.HandleFunc("/telegram-webhook", telegramWebhookHandler).Methods("POST")
	router.PathPrefix("/").HandlerFunc(spaHandler)

	srv := &http.Server{
		Addr:    ":" + *port,
		Handler: router,
	}

	// Канал для получения сигналов операционной системы
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Запускаем сервер в отдельной горутине
	go func() {
		logger.Infof("Сервис запущен на порту :%s", *port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Ошибка запуска сервера: %v", err)
		}
	}()

	// Ожидаем сигнал завершения
	<-done
	logger.Info("Получен сигнал завершения, начинаем graceful shutdown...")

	// Создаем контекст с таймаутом для graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Errorf("Ошибка при graceful shutdown: %v", err)
		os.Exit(1)
	}

	logger.Info("Сервер успешно остановлен")
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
	provider := selectWeightedProvider()
	logger.Infof("Выбран провайдер: %T", provider)

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	joke, err := provider.FetchJoke(ctx)
	if err != nil {
		logger.Errorf("Ошибка получения анекдота от провайдера %T: %v", provider, err)
		http.Error(w, "Анекдоты временно недоступны", http.StatusInternalServerError)
		return
	}

	logger.Infof("Получен анекдот от %s: %s", joke.Source, joke.Text)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(joke); err != nil {
		logger.Errorf("Ошибка сериализации анекдота в JSON: %v", err)
		http.Error(w, "Ошибка сервера", http.StatusInternalServerError)
		return
	}
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
