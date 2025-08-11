package gpt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type GroqClient struct {
	APIKey string
}
type ImageRequest struct {
	Prompt string `json:"prompt"`
	N      int    `json:"n"`
	Size   string `json:"size"`
}

type ImageResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		URL string `json:"url"`
	} `json:"data"`
}

func NewGroqClient() *GroqClient {
	return &GroqClient{
		APIKey: os.Getenv("GROQ_API_KEY"),
	}
}

type groqRequest struct {
	Model       string        `json:"model"`
	Messages    []groqMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
}

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqResponse struct {
	Choices []struct {
		Message groqMessage `json:"message"`
	} `json:"choices"`
}

func (c *GroqClient) GenerateText(prompt string) (string, error) {
	url := "https://api.openai.com/v1/chat/completions"

	reqBody := groqRequest{
		Model: "gpt-4", // или "gpt-4" - в зависимости от твоего доступа
		Messages: []groqMessage{
			{Role: "system", Content: "Ты помощник, который пишет креативные и интересные тексты для постов в Телеграм. ..."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   800,
		Temperature: 0.8,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ошибка маршалинга запроса: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("неожиданный статус %d: %s", resp.StatusCode, string(body))
	}

	var res groqResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", fmt.Errorf("ошибка декодирования ответа: %w", err)
	}

	if len(res.Choices) == 0 {
		return "", fmt.Errorf("пустой ответ от GPT")
	}

	return res.Choices[0].Message.Content, nil
}
func GenerateImage(apiKey string, prompt string) (string, error) {
	url := "https://api.openai.com/v1/images/generations"

	reqBody := ImageRequest{
		Prompt: prompt,
		N:      1,
		Size:   "512x512",
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	client := http.DefaultClient
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var res ImageResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}

	if len(res.Data) == 0 {
		return "", fmt.Errorf("пустой ответ от генерации изображения")
	}

	return res.Data[0].URL, nil
}
func (c *GroqClient) GenerateImage(prompt string) (string, error) {
	url := "https://api.openai.com/v1/images/generations"

	reqBody := map[string]interface{}{
		"model":   "dall-e-3", // или нужная модель для генерации картинок
		"prompt":  prompt,
		"n":       1,
		"size":    "512x768", // или "1024x1024" или "768x1024" (портретный 3:4)
		"quality": 2,         // если поддерживается, можно добавить
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var res struct {
		Created int64 `json:"created"`
		Data    []struct {
			URL string `json:"url"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}

	if len(res.Data) == 0 {
		return "", fmt.Errorf("пустой ответ от генерации изображения")
	}

	return res.Data[0].URL, nil
}
