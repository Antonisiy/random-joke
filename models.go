package main

import (
	"context"
)

// Joke представляет структуру анекдота
type Joke struct {
	Text      string `json:"joke"`
	Source    string `json:"source"`
	IsRussian bool   `json:"is_russian"`
}

// JokeProvider описывает интерфейс для получения анекдота
type JokeProvider interface {
	FetchJoke(ctx context.Context) (Joke, error)
}
