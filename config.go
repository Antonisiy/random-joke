package main

import (
	"os"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// Configuration constants and variables
const (
	Version = "1.0.0"
)

var (
	// Глобальный логгер
	logger = logrus.New()

	// Глобальная конфигурация
	appConfig *Config

	// Все доступные провайдеры анекдотов
	jokeProviders = []JokeProvider{
		RzhunemoguProvider{}, // Русский источник
		AnekdotRuProvider{},  // Русский источник
		BaneksProvider{},     // Русский источник
		DadJokeProvider{},    // Английский источник
		JokeAPIProvider{},    // Английский источник
	}

	// Веса для источников (русские источники имеют больший вес)
	providerWeights = []int{
		3, // RzhunemoguProvider
		3, // AnekdotRuProvider
		3, // BaneksProvider
		1, // DadJokeProvider
		1, // JokeAPIProvider
	}

	// Разрешенные CORS origins
	allowedOrigins = []string{
		"http://localhost:5173",
		"http://localhost",
		"https://welcome-cattle-regular.ngrok-free.app",
	}

	// Память для хранения последних анекдотов в Telegram
	jokeMemory map[int64]string
)

type Config struct {
	TelegramBotToken string `yaml:"telegram_bot_token"`
}

func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
