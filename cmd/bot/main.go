package main

import (
	"lady/config"
	"lady/internal/gpt"
	"lady/internal/repository"
	"lady/internal/tg"
	"lady/internal/usecase"
	"log"
	"os"
)

func main() {
	cfg := config.LoadConfig()

	repo := repository.NewTopicRepository(cfg.DBPath)
	uc := usecase.NewTopicUsecase(repo)

	gptClient := gpt.NewGroqClient()
	tuc := usecase.NewGenerateUsecase(gptClient)
	f, err := os.OpenFile("bot.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	bot := tg.NewBot(cfg.BotToken, uc, tuc, cfg.OpenAIAPIKey)
	bot.Start()

}
