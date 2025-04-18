package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

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
	logger.Info("Fetching joke from rzhunemogu.ru")
	url := "http://rzhunemogu.ru/RandJSON.aspx?CType=1"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return Joke{}, fmt.Errorf("ошибка создания запроса: %v", err)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return Joke{}, fmt.Errorf("ошибка выполнения запроса: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Joke{}, fmt.Errorf("ошибка чтения тела ответа: %v", err)
	}

	body, err = decodeWindows1251(body)
	if err != nil {
		return Joke{}, fmt.Errorf("ошибка декодирования windows-1251: %v", err)
	}

	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	body = bytes.TrimSpace(body)
	body = bytes.ReplaceAll(body, []byte("\r\n"), []byte("\\n"))

	logger.Infof("Response from rzhunemogu.ru (decoded): %s", string(body))

	var result struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return Joke{}, fmt.Errorf("ошибка разбора JSON: %v, тело: %s", err, string(body))
	}

	if result.Content == "" {
		return Joke{}, fmt.Errorf("получен пустой анекдот от rzhunemogu.ru")
	}

	result.Content = strings.ReplaceAll(result.Content, "\\n", "\n")
	result.Content = strings.TrimSpace(result.Content)

	logger.Info("Successfully fetched joke from rzhunemogu.ru")
	return Joke{Text: result.Content, Source: "rzhunemogu.ru", IsRussian: true}, nil
}

// AnekdotRuProvider для anekdot.ru
type AnekdotRuProvider struct{}

func (p AnekdotRuProvider) FetchJoke(ctx context.Context) (Joke, error) {
	logger.Info("Fetching joke from anekdot.ru")
	url := "https://www.anekdot.ru/rss/randomu.html"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return Joke{}, fmt.Errorf("ошибка создания запроса: %v", err)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return Joke{}, fmt.Errorf("ошибка выполнения запроса: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Joke{}, fmt.Errorf("ошибка чтения тела ответа: %v", err)
	}

	content := string(body)

	start := strings.Index(content, "JSON.parse('[")
	if start == -1 {
		return Joke{}, fmt.Errorf("не удалось найти начало массива анекдотов")
	}
	start += len("JSON.parse('[")

	end := strings.Index(content[start:], "]')")
	if end == -1 {
		return Joke{}, fmt.Errorf("не удалось найти конец массива анекдотов")
	}

	jokesStr := content[start : start+end]
	jokes := strings.Split(jokesStr, "\\\",\\\"")
	if len(jokes) == 0 {
		return Joke{}, fmt.Errorf("не найдено ни одного анекдота")
	}

	rand.Seed(time.Now().UnixNano())
	joke := jokes[rand.Intn(len(jokes))]

	joke = strings.ReplaceAll(joke, "\\\"", "\"")
	joke = strings.ReplaceAll(joke, "<br>", "\n")
	joke = strings.ReplaceAll(joke, "&quot;", "\"")
	joke = strings.ReplaceAll(joke, "&lt;", "<")
	joke = strings.ReplaceAll(joke, "&gt;", ">")
	joke = strings.ReplaceAll(joke, "&amp;", "&")
	joke = strings.TrimSpace(joke)

	if joke == "" {
		return Joke{}, fmt.Errorf("получен пустой анекдот от anekdot.ru")
	}

	logger.Info("Successfully fetched joke from anekdot.ru")
	return Joke{Text: joke, Source: "anekdot.ru", IsRussian: true}, nil
}

// BaneksProvider для baneks.ru
type BaneksProvider struct{}

func (p BaneksProvider) FetchJoke(ctx context.Context) (Joke, error) {
	logger.Info("Fetching joke from baneks.ru")
	url := "https://baneks.ru/random"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		logger.Errorf("Ошибка создания запроса к baneks.ru: %v", err)
		return Joke{}, err
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logger.Errorf("Ошибка выполнения запроса к baneks.ru: %v", err)
		return Joke{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Errorf("Ошибка чтения тела ответа от baneks.ru: %v", err)
		return Joke{}, err
	}

	content := string(body)
	logger.Debugf("Получен ответ от baneks.ru (первые 500 символов): %s", content[:min(500, len(content))])

	// Ищем meta description
	descStart := strings.Index(content, `<meta name="description" content="`)
	if descStart == -1 {
		logger.Error("Не найден meta description в ответе")
		return Joke{}, fmt.Errorf("не удалось найти анекдот")
	}
	descStart += len(`<meta name="description" content="`)
	logger.Debugf("Найдена позиция начала анекдота: %d", descStart)

	descEnd := strings.Index(content[descStart:], `">`)
	if descEnd == -1 {
		logger.Error("Не найден закрывающий тег meta description")
		return Joke{}, fmt.Errorf("не удалось найти конец анекдота")
	}
	logger.Debugf("Найдена позиция конца анекдота: %d", descEnd)

	joke := content[descStart : descStart+descEnd]
	joke = strings.ReplaceAll(joke, "\\n", "\n")
	joke = strings.ReplaceAll(joke, "\\\"", "\"")
	joke = strings.TrimSpace(joke)

	logger.Infof("Успешно получен анекдот от baneks.ru: %s", joke)
	return Joke{Text: joke, Source: "baneks.ru", IsRussian: true}, nil
}

// Вспомогательная функция для определения минимального значения
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

// selectWeightedProvider выбирает провайдер анекдотов с учетом весов
func selectWeightedProvider() JokeProvider {
	totalWeight := 0
	for _, w := range providerWeights {
		totalWeight += w
	}

	r := rand.Intn(totalWeight)
	curr := 0
	for i, w := range providerWeights {
		curr += w
		if r < curr {
			return jokeProviders[i]
		}
	}
	return jokeProviders[0]
}
