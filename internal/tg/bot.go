package tg

import (
	"fmt"
	"lady/internal/usecase"
	"log"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Bot представляет Telegram-бота.
type Bot struct {
	api     *tgbotapi.BotAPI
	handler *Handler
	usecase *usecase.TopicUsecase
}

// NewBot создает новый экземпляр бота.
func NewBot(token string, uc *usecase.TopicUsecase, tuc *usecase.GenerateUsecase, apiKey string) *Bot {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}
	_, err = bot.Request(tgbotapi.DeleteWebhookConfig{})
	if err != nil {
		log.Fatalf("Failed to delete webhook: %v", err)
	}
	handler := NewHandler(bot, uc, tuc, apiKey)
	return &Bot{api: bot, handler: handler, usecase: uc}
}

// Start запускает бота и фоновую задачу для отложенных постов.
func (b *Bot) Start() {
	// Запускаем фоновую задачу для проверки отложенных постов
	go b.runScheduledPosts()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			if update.Message.IsCommand() {
				b.handler.HandleCommand(update)
				continue
			}
			if update.Message.Document != nil {
				b.handler.HandleFile(update)
				continue
			}
			if update.Message.Text != "" {
				b.handler.HandleText(update)
			}
		} else if update.CallbackQuery != nil {
			b.handler.HandleCallback(update)
		}
	}
}

// runScheduledPosts проверяет и публикует отложенные посты с фотографиями.
func (b *Bot) runScheduledPosts() {
	const channelID = "-1002848619245"
	// Convert channelID to int64
	channelIDInt, err := strconv.ParseInt(channelID, 10, 64)
	if err != nil {
		log.Fatalf("Failed to parse channelID %s to int64: %v", channelID, err)
	}
	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		log.Printf("Проверка отложенных постов на %s", time.Now().Format("02.01.2006 15:04:05"))
		posts := b.usecase.GetScheduledPosts()
		if len(posts) == 0 {
			log.Printf("Нет постов для публикации")
			continue
		}
		for chatID, post := range posts {
			log.Printf("Обработка поста для chatID %d, запланированного на %s", chatID, post.PublishAt.Format("02.01.2006 15:04:05"))
			if post.Img1 == "" || post.Img2 == "" {
				log.Printf("Ошибка: изображения отсутствуют для chatID %d", chatID)
				continue
			}
			// Truncate caption to 1024 characters
			caption := post.Text
			if len(caption) > 1024 {
				caption = caption[:1024]
				log.Printf("Текст поста для chatID %d укорочен до 1024 символов", chatID)
			}
			log.Printf("Длина подписи для chatID %d: %d символов", chatID, len(caption))
			// Создаем медиа-группу для текста и фотографий
			mediaGroup := tgbotapi.NewMediaGroup(channelIDInt, []interface{}{
				tgbotapi.NewInputMediaPhoto(tgbotapi.FileURL(post.Img1)),
				tgbotapi.NewInputMediaPhoto(tgbotapi.FileURL(post.Img2)),
			})
			// Correct type assertion
			media := mediaGroup.Media[0].(tgbotapi.InputMediaPhoto)
			media.Caption = caption
			mediaGroup.Media[0] = media
			if _, err := b.api.Send(mediaGroup); err != nil {
				log.Printf("Ошибка публикации медиа-группы для chatID %d: %v", chatID, err)
				continue
			}
			if err := b.usecase.ClearPendingPost(chatID); err != nil {
				log.Printf("Ошибка очистки поста для chatID %d: %v", chatID, err)
			} else {
				log.Printf("Пост для chatID %d успешно опубликован и очищен", chatID)
				notifyMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Ваш пост с фотографиями опубликован в канале на %s", time.Now().Format("02.01.2006 15:04")))
				if len(post.Text) > 1024 {
					notifyMsg.Text += "\nВнимание: текст поста был укорочен из-за ограничений Telegram."
				}
				if _, err := b.api.Send(notifyMsg); err != nil {
					log.Printf("Ошибка отправки уведомления пользователю chatID %d: %v", chatID, err)
				}
			}
		}
	}
}
