package main

import (
	"github.com/sirupsen/logrus"
)

// Configuration constants and variables
const (
	Version = "1.0.0"
)

var (
	// Глобальный логгер
	logger = logrus.New()

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
