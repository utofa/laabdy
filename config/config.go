package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	BotToken     string
	DBPath       string
	OpenAIAPIKey string
}

func LoadConfig() *Config {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Ошибка загрузки .env  файла")
	}
	return &Config{
		BotToken:     os.Getenv("BOT_TOKEN"),
		DBPath:       os.Getenv("DB_PATH"),
		OpenAIAPIKey: os.Getenv("GROQ_API_KEY"),
	}
}
