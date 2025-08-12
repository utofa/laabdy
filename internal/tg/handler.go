package tg

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"lady/internal/gpt"
	"lady/internal/usecase"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const channelID = "-1002848619245" // ID канала для публикации

// Handler обрабатывает входящие обновления Telegram.
type Handler struct {
	api             *tgbotapi.BotAPI
	usecase         *usecase.TopicUsecase
	generateUsecase *usecase.GenerateUsecase
	apiKey          string
}

// NewHandler создает новый экземпляр Handler.
func NewHandler(api *tgbotapi.BotAPI, uc *usecase.TopicUsecase, tuc *usecase.GenerateUsecase, apiKey string) *Handler {
	return &Handler{api: api, usecase: uc, generateUsecase: tuc, apiKey: apiKey}
}

// extractSentences разбивает текст на предложения.
func extractSentences(text string) []string {
	re := regexp.MustCompile(`[.!?]\s+`)
	sentences := re.Split(text, -1)
	var res []string
	for _, s := range sentences {
		s = strings.TrimSpace(s)
		if s != "" {
			res = append(res, s+".")
		}
	}
	return res
}

// extractStart извлекает начальную часть текста (первые два предложения).
func extractStart(text string) string {
	sentences := extractSentences(text)
	if len(sentences) < 2 {
		return text
	}
	return strings.Join(sentences[:2], " ")
}

// extractMiddle извлекает среднюю часть текста.
func extractMiddle(text string) string {
	sentences := extractSentences(text)
	n := len(sentences)
	if n < 4 {
		return strings.Join(sentences[n/2:], " ")
	}
	midStart := n / 3
	midEnd := midStart + 2
	if midEnd > n {
		midEnd = n
	}
	return strings.Join(sentences[midStart:midEnd], " ")
}

// HandleCommand обрабатывает команды.
func (h *Handler) HandleCommand(update tgbotapi.Update) {
	chatID := update.Message.Chat.ID
	command := update.Message.Command()
	args := strings.TrimSpace(update.Message.CommandArguments())

	// Convert channelID to int64
	channelIDInt, err := strconv.ParseInt(channelID, 10, 64)
	if err != nil {
		log.Printf("Ошибка преобразования channelID %s в int64: %v", channelID, err)
		h.api.Send(tgbotapi.NewMessage(chatID, "Внутренняя ошибка сервера"))
		return
	}

	switch command {
	case "start":
		h.api.Send(tgbotapi.NewMessage(chatID, "Привет! Просто пришли мне тему, и я сгенерирую текст."))
	case "list":
		topics, err := h.usecase.ListTopics()
		if err != nil {
			h.api.Send(tgbotapi.NewMessage(chatID, "Ошибка при получении тем"))
			log.Printf("Ошибка получения тем: %v", err)
			return
		}
		if len(topics) == 0 {
			h.api.Send(tgbotapi.NewMessage(chatID, "Темы не найдены"))
			return
		}
		var builder strings.Builder
		builder.WriteString("Сохранённые темы:\n")
		for _, t := range topics {
			builder.WriteString(fmt.Sprintf("- %s\n", t.Title))
		}
		h.api.Send(tgbotapi.NewMessage(chatID, builder.String()))

	case "generate":
		if args == "" {
			h.api.Send(tgbotapi.NewMessage(chatID, "Укажи тему: /generate <тема>"))
			return
		}
		text, img1, img2, err := h.generatePostContent(args)
		if err != nil {
			h.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка генерации: %v", err)))
			return
		}
		log.Printf("Сгенерирован пост для chatID %d: Текст: %s, Фото1: %s, Фото2: %s", chatID, text, img1, img2)
		msg := tgbotapi.NewMessage(chatID, text)
		btn := tgbotapi.NewInlineKeyboardButtonData("Опубликовать", "publish")
		btn1 := tgbotapi.NewInlineKeyboardButtonData("Редактировать", "edit")
		btn2 := tgbotapi.NewInlineKeyboardButtonData("Запланировать", "schedule")
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(btn, btn1, btn2))
		if _, err := h.api.Send(msg); err != nil {
			log.Printf("Ошибка отправки сообщения: %v", err)
		}
		// Сохраняем пост с фотографиями
		if err := h.usecase.SavePendingPost(chatID, text, img1, img2, time.Time{}); err != nil {
			log.Printf("Ошибка сохранения отложенного поста для chatID %d: %v", chatID, err)
		}
		h.api.Send(tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(img1)))
		h.api.Send(tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(img2)))

	case "publish_pending":
		pendingPost, img1, img2, publishAt, err := h.usecase.GetPendingPost(chatID)
		if err != nil || pendingPost == "" {
			h.api.Send(tgbotapi.NewMessage(chatID, "Нет отложенных постов"))
			return
		}
		if publishAt.IsZero() {
			if img1 == "" || img2 == "" {
				h.api.Send(tgbotapi.NewMessage(chatID, "Ошибка: изображения для поста отсутствуют"))
				log.Printf("Ошибка: изображения отсутствуют для chatID %d", chatID)
				return
			}
			// Truncate caption to 1024 characters
			caption := pendingPost
			if len(caption) > 1024 {
				caption = caption[:1024]
				log.Printf("Текст поста для chatID %d укорочен до 1024 символов", chatID)
			}
			log.Printf("Длина подписи для chatID %d: %d символов", chatID, len(caption))
			mediaGroup := tgbotapi.NewMediaGroup(channelIDInt, []interface{}{
				tgbotapi.NewInputMediaPhoto(tgbotapi.FileURL(img1)),
				tgbotapi.NewInputMediaPhoto(tgbotapi.FileURL(img2)),
			})
			// Correct type assertion
			media := mediaGroup.Media[0].(tgbotapi.InputMediaPhoto)
			media.Caption = caption
			mediaGroup.Media[0] = media
			if _, err := h.api.Send(mediaGroup); err != nil {
				h.api.Send(tgbotapi.NewMessage(chatID, "Ошибка публикации отложенного поста"))
				log.Printf("Ошибка публикации: %v", err)
			} else {
				notifyMsg := tgbotapi.NewMessage(chatID, "Отложенный пост с фотографиями опубликован!")
				if len(pendingPost) > 1024 {
					notifyMsg.Text += "\nВнимание: текст поста был укорочен из-за ограничений Telegram."
				}
				h.api.Send(notifyMsg)
				h.usecase.ClearPendingPost(chatID)
			}
		} else {
			h.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Пост запланирован на %s", publishAt.Format("02.01.2006 15:04"))))
		}

	case "schedule":
		if args == "" {
			h.api.Send(tgbotapi.NewMessage(chatID, "Укажи дату и время: /schedule <DD.MM.YYYY HH:MM> (например, 11.08.2025 17:30)"))
			return
		}
		pendingPost, img1, img2, _, err := h.usecase.GetPendingPost(chatID)
		if err != nil || pendingPost == "" {
			h.api.Send(tgbotapi.NewMessage(chatID, "Нет отложенного поста для планирования. Сначала сгенерируйте пост."))
			return
		}
		log.Printf("Попытка запланировать пост для chatID %d с временем: %s", chatID, args)
		// Use Asia/Novosibirsk timezone
		loc, err := time.LoadLocation("Asia/Novosibirsk")
		if err != nil {
			log.Printf("Ошибка загрузки часового пояса Asia/Novosibirsk: %v", err)
			h.api.Send(tgbotapi.NewMessage(chatID, "Внутренняя ошибка сервера"))
			return
		}
		publishAt, err := time.ParseInLocation("02.01.2006 15:04", args, loc)
		if err != nil {
			h.api.Send(tgbotapi.NewMessage(chatID, "Неверный формат даты и времени. Используйте: DD.MM.YYYY HH:MM (например, 11.08.2025 17:30)"))
			log.Printf("Ошибка парсинга времени '%s': %v", args, err)
			return
		}
		if err := h.usecase.SavePendingPost(chatID, pendingPost, img1, img2, publishAt); err != nil {
			h.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка при планировании поста: %v", err)))
			log.Printf("Ошибка сохранения отложенного поста для chatID %d: %v", chatID, err)
		} else {
			h.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Пост с фотографиями запланирован на %s", publishAt.Format("02.01.2006 15:04"))))
			log.Printf("Пост для chatID %d запланирован на %s", chatID, publishAt.Format("02.01.2006 15:04"))
		}

	case "list_pending": // Added for debugging
		pendingPost, img1, img2, publishAt, err := h.usecase.GetPendingPost(chatID)
		if err != nil || pendingPost == "" {
			h.api.Send(tgbotapi.NewMessage(chatID, "Нет отложенных постов"))
			return
		}
		msg := fmt.Sprintf("Отложенный пост:\nТекст: %s\nДлина текста: %d символов\nФото1: %s\nФото2: %s\nВремя: %s", pendingPost, len(pendingPost), img1, img2, publishAt.Format("02.01.2006 15:04"))
		h.api.Send(tgbotapi.NewMessage(chatID, msg))

	default:
		h.api.Send(tgbotapi.NewMessage(chatID, "Неизвестная команда."))
	}
	log.Printf("Received command: %s", command)
}

// HandleText обрабатывает текстовые сообщения.
func (h *Handler) HandleText(update tgbotapi.Update) {
	chatID := update.Message.Chat.ID
	text := strings.TrimSpace(update.Message.Text)
	if len(text) < 3 {
		h.api.Send(tgbotapi.NewMessage(chatID, "Тема слишком короткая, попробуйте другую"))
		return
	}

	// Проверяем, ожидается ли редактирование
	if pendingEdit, messageID, err := h.usecase.GetPendingEdit(chatID); err == nil && pendingEdit != "" {
		editMsg := tgbotapi.NewEditMessageTextAndMarkup(
			chatID,
			messageID,
			text,
			tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("Опубликовать", "publish"),
					tgbotapi.NewInlineKeyboardButtonData("Редактировать", "edit"),
					tgbotapi.NewInlineKeyboardButtonData("Запланировать", "schedule"),
				),
			),
		)
		if _, err := h.api.Request(editMsg); err != nil {
			h.api.Send(tgbotapi.NewMessage(chatID, "Ошибка обновления текста"))
			log.Printf("Ошибка редактирования: %v", err)
		} else {
			h.api.Send(tgbotapi.NewMessage(chatID, "Текст поста обновлен!"))
			h.usecase.ClearPendingEdit(chatID)
		}
		return
	}

	// Сохраняем новую тему
	if err := h.usecase.AddTopic(text); err != nil {
		h.api.Send(tgbotapi.NewMessage(chatID, "Ошибка сохранения темы"))
		log.Printf("Ошибка сохранения темы: %v", err)
		return
	}

	h.api.Send(tgbotapi.NewMessage(chatID, "Тема сохранена! Генерируем текст..."))

	// Эффект печати
	if _, err := h.api.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)); err != nil {
		log.Printf("Ошибка отправки ChatAction: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)

	// Генерируем контент
	text, img1, img2, err := h.generatePostContent(text)
	if err != nil {
		h.api.Send(tgbotapi.NewMessage(chatID, err.Error()))
		return
	}

	// Отправляем сгенерированный текст с кнопками
	msg := tgbotapi.NewMessage(chatID, text)
	btn := tgbotapi.NewInlineKeyboardButtonData("Опубликовать", "publish")
	btn1 := tgbotapi.NewInlineKeyboardButtonData("Редактировать", "edit")
	btn2 := tgbotapi.NewInlineKeyboardButtonData("Запланировать", "schedule")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(btn, btn1, btn2))
	if _, err := h.api.Send(msg); err != nil {
		log.Printf("Ошибка отправки сообщения: %v", err)
	}

	// Сохраняем пост с фотографиями как отложенный
	if err := h.usecase.SavePendingPost(chatID, text, img1, img2, time.Time{}); err != nil {
		log.Printf("Ошибка сохранения отложенного поста для chatID %d: %v", chatID, err)
	}

	// Отправляем картинки
	h.api.Send(tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(img1)))
	h.api.Send(tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(img2)))
}

// HandleFile обрабатывает загруженные файлы.
func (h *Handler) HandleFile(update tgbotapi.Update) {
	chatID := update.Message.Chat.ID
	fileID := update.Message.Document.FileID
	fileName := update.Message.Document.FileName

	file, err := h.api.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		h.api.Send(tgbotapi.NewMessage(chatID, "Не удалось получить файл"))
		log.Printf("Ошибка получения файла: %v", err)
		return
	}

	// Скачиваем файл
	url := file.Link(h.api.Token)
	tmpFile := "temp_topics" + filepath.Ext(fileName)
	if err := downloadFile(tmpFile, url); err != nil {
		h.api.Send(tgbotapi.NewMessage(chatID, "Ошибка скачивания файла"))
		log.Printf("Ошибка скачивания файла: %v", err)
		return
	}
	defer os.Remove(tmpFile)

	ext := strings.ToLower(filepath.Ext(fileName))
	var count int
	switch ext {
	case ".txt":
		count = h.importFromTXT(tmpFile)
	case ".csv":
		count = h.importFromCSV(tmpFile)
	default:
		h.api.Send(tgbotapi.NewMessage(chatID, "Поддерживаются только файлы .txt и .csv"))
		return
	}

	h.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Импортировано %d тем", count)))
}

// importFromTXT импортирует темы из текстового файла.
func (h *Handler) importFromTXT(filePath string) int {
	f, err := os.Open(filePath)
	if err != nil {
		log.Printf("Ошибка открытия TXT файла: %v", err)
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var count int
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) < 3 {
			continue
		}
		if err := h.usecase.AddTopic(line); err == nil {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Ошибка чтения TXT файла: %v", err)
	}
	return count
}

// importFromCSV импортирует темы из CSV файла.
func (h *Handler) importFromCSV(filePath string) int {
	f, err := os.Open(filePath)
	if err != nil {
		log.Printf("Ошибка открытия CSV файла: %v", err)
		return 0
	}
	defer f.Close()

	reader := csv.NewReader(f)
	var count int
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) == 0 {
			log.Printf("Ошибка чтения CSV записи: %v", err)
			continue
		}
		topic := strings.TrimSpace(record[0])
		if len(topic) < 3 {
			continue
		}
		if err := h.usecase.AddTopic(topic); err == nil {
			count++
		}
	}
	return count
}

// downloadFile скачивает файл по URL.
func downloadFile(filepath string, url string) error {
	out, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("ошибка создания файла: %w", err)
	}
	defer out.Close()

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("ошибка загрузки файла: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("неверный статус ответа: %d", resp.StatusCode)
	}

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("ошибка сохранения файла: %w", err)
	}
	return nil
}

// HandleCallback обрабатывает callback-запросы от кнопок.
func (h *Handler) HandleCallback(update tgbotapi.Update) {
	data := update.CallbackQuery.Data
	chatID := update.CallbackQuery.Message.Chat.ID
	messageID := update.CallbackQuery.Message.MessageID
	msgText := update.CallbackQuery.Message.Text

	// Convert channelID to int64
	channelIDInt, err := strconv.ParseInt(channelID, 10, 64)
	if err != nil {
		log.Printf("Ошибка преобразования channelID %s в int64: %v", channelID, err)
		h.api.Send(tgbotapi.NewMessage(chatID, "Внутренняя ошибка сервера"))
		return
	}

	switch data {
	case "publish":
		_, img1, img2, _, err := h.usecase.GetPendingPost(chatID)
		if err != nil {
			log.Printf("Ошибка получения отложенного поста для chatID %d: %v", chatID, err)
			img1, img2 = "", ""
		}
		if img1 == "" || img2 == "" {
			h.api.Send(tgbotapi.NewMessage(chatID, "Ошибка: изображения для поста отсутствуют"))
			return
		}

		caption := msgText
		if len(caption) > 1024 {
			caption = caption[:1024]
			log.Printf("Текст поста укорочен до 1024 символов")
		}

		mediaGroup := tgbotapi.NewMediaGroup(channelIDInt, []interface{}{
			tgbotapi.NewInputMediaPhoto(tgbotapi.FileURL(img1)),
			tgbotapi.NewInputMediaPhoto(tgbotapi.FileURL(img2)),
		})

		media := mediaGroup.Media[0].(tgbotapi.InputMediaPhoto)
		media.Caption = caption
		mediaGroup.Media[0] = media

		if _, err := h.api.Send(mediaGroup); err != nil {
			h.api.Send(tgbotapi.NewMessage(chatID, "Ошибка публикации в канал"))
			log.Printf("Ошибка публикации: %v", err)
		} else {
			notifyMsg := tgbotapi.NewMessage(chatID, "Пост с фотографиями успешно опубликован в канале!")
			if len(msgText) > 1024 {
				notifyMsg.Text += "\nВнимание: текст был укорочен."
			}
			h.api.Send(notifyMsg)
			h.usecase.ClearPendingPost(chatID)
		}

		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "Опубликовано")
		h.api.Request(callback)

	case "edit":
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "Редактирование")
		h.api.Request(callback)
		h.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Текущий текст:\n%s\n\nОтправьте новый текст для замены.", msgText)))
		if err := h.usecase.SavePendingEdit(chatID, messageID, msgText); err != nil {
			h.api.Send(tgbotapi.NewMessage(chatID, "Ошибка при сохранении данных для редактирования"))
			log.Printf("Ошибка сохранения редактирования: %v", err)
		}

	case "schedule":
		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "Планирование")
		h.api.Request(callback)

		_, img1, img2, _, err := h.usecase.GetPendingPost(chatID)
		if err != nil {
			log.Printf("Ошибка получения отложенного поста: %v", err)
			img1, img2 = "", ""
		}
		if err := h.usecase.SavePendingPost(chatID, msgText, img1, img2, time.Time{}); err != nil {
			h.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка при сохранении поста: %v", err)))
		} else {
			h.api.Send(tgbotapi.NewMessage(chatID, "Отправьте дату и время публикации (DD.MM.YYYY HH:MM): /schedule <дата и время> (например, 11.08.2025 17:30)"))
		}
	}
}

// generatePostContent генерирует текст и изображения для поста.
func (h *Handler) generatePostContent(topic string) (string, string, string, error) {
	text, err := h.generateUsecase.GenerateFromTopic(topic)
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка генерации текста: %w", err)
	}

	startPrompt := extractStart(text)
	middlePrompt := extractMiddle(text)

	img1, err := gpt.GenerateImage(h.apiKey, startPrompt)
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка генерации первой картинки: %w", err)
	}
	img2, err := gpt.GenerateImage(h.apiKey, middlePrompt)
	if err != nil {
		return "", "", "", fmt.Errorf("ошибка генерации второй картинки: %w", err)
	}

	return text, img1, img2, nil
}
