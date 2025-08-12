package usecase

import (
	"errors"
	"fmt"
	"lady/internal/domain"
	"lady/internal/gpt"
	"lady/internal/repository"
	"strings"
	"sync"
	"time"
)

// TopicUsecase управляет темами и их состоянием.
type TopicUsecase struct {
	repo         *repository.TopicRepository
	pendingEdits map[int64]struct {
		Text      string
		MessageID int
	}
	pendingPosts map[int64]struct {
		Text      string
		Img1      string // URL первой фотографии
		Img2      string // URL второй фотографии
		PublishAt time.Time
	}
	pendingSchedule map[int64]bool
	mu              sync.RWMutex
}

// GenerateUsecase управляет генерацией текстов.
type GenerateUsecase struct {
	gpt *gpt.GroqClient
}

// NewTopicUsecase создает новый экземпляр TopicUsecase.
func NewTopicUsecase(r *repository.TopicRepository) *TopicUsecase {
	return &TopicUsecase{
		repo: r,
		pendingEdits: make(map[int64]struct {
			Text      string
			MessageID int
		}),
		pendingPosts: make(map[int64]struct {
			Text      string
			Img1      string
			Img2      string
			PublishAt time.Time
		}),
		pendingSchedule: make(map[int64]bool),
	}
}

// NewGenerateUsecase создает новый экземпляр GenerateUsecase.
func NewGenerateUsecase(gptClient *gpt.GroqClient) *GenerateUsecase {
	return &GenerateUsecase{gpt: gptClient}
}

// AddTopic добавляет новую тему.
func (u *TopicUsecase) AddTopic(title string) error {
	title = strings.TrimSpace(title)
	if len(title) < 3 {
		return fmt.Errorf("тема слишком короткая")
	}
	return u.repo.Save(domain.Topic{Title: title})
}

// ListTopics возвращает список всех тем.
func (u *TopicUsecase) ListTopics() ([]domain.Topic, error) {
	return u.repo.List()
}

// GenerateFromTopic генерирует текст на основе темы.
func (u *GenerateUsecase) GenerateFromTopic(topic string) (string, error) {
	if topic == "" {
		return "", errors.New("тема не может быть пустой")
	}
	prompt := fmt.Sprintf(
		"Ты — креативный копирайтер, который пишет короткие чувственные тексты для постов в Телеграм. "+
			"Стиль — интимный, будто автор говорит лично с читателем, с лёгкой игрой и флиртом, загадочностью и недосказанностью. "+
			"Используй метафоры, сенсорные детали (взгляд, прикосновение, звук), эмоциональные контрасты. "+
			"Тексты должны быть живыми, с короткими и длинными фразами, создавая ощущение разговора «один на один». "+
			"В начале всегда интригующая фраза-приманка, в середине — эмоциональный пик с игрой образов, в конце — крючок, который заставляет ждать продолжения. "+
			"Эмодзи используй аккуратно, чтобы усиливать настроение, а не просто вставлять. "+
			"Избегай прямых объяснений — передавай чувства через образы и действия. "+
			"Пиши от первого лица, от женского лица, с уверенностью, мягкой провокацией и тайной. "+
			"Сгенерируй текст с длинной 1024 символов в таком стиле по теме: %s", topic)
	return u.gpt.GenerateText(prompt)
}

// SavePendingEdit сохраняет данные для редактирования.
func (u *TopicUsecase) SavePendingEdit(chatID int64, messageID int, text string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if text == "" {
		return errors.New("текст для редактирования не может быть пустым")
	}
	u.pendingEdits[chatID] = struct {
		Text      string
		MessageID int
	}{Text: text, MessageID: messageID}
	return nil
}

// GetPendingEdit получает данные для редактирования.
func (u *TopicUsecase) GetPendingEdit(chatID int64) (string, int, error) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	edit, exists := u.pendingEdits[chatID]
	if !exists {
		return "", 0, errors.New("нет данных для редактирования")
	}
	return edit.Text, edit.MessageID, nil
}

// ClearPendingEdit очищает данные редактирования.
func (u *TopicUsecase) ClearPendingEdit(chatID int64) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, exists := u.pendingEdits[chatID]; !exists {
		return errors.New("нет данных для очистки")
	}
	delete(u.pendingEdits, chatID)
	return nil
}

// SavePendingPost сохраняет пост для отложенной публикации с указанием времени и фотографий.
func (u *TopicUsecase) SavePendingPost(chatID int64, text, img1, img2 string, publishAt time.Time) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if text == "" {
		return errors.New("текст поста не может быть пустым")
	}
	if !publishAt.IsZero() {
		// Допускаем планирование на ближайшие 2 минуты
		if publishAt.Before(time.Now().Add(-2 * time.Minute)) {
			return fmt.Errorf("время публикации (%s) не может быть в прошлом (текущее время: %s)", publishAt.Format("02.01.2006 15:04"), time.Now().Format("02.01.2006 15:04"))
		}
	}
	u.pendingPosts[chatID] = struct {
		Text, Img1, Img2 string
		PublishAt        time.Time
	}{Text: text, Img1: img1, Img2: img2, PublishAt: publishAt}
	return nil
}

// GetPendingPost получает отложенный пост.
func (u *TopicUsecase) GetPendingPost(chatID int64) (string, string, string, time.Time, error) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	post, exists := u.pendingPosts[chatID]
	if !exists {
		return "", "", "", time.Time{}, errors.New("нет отложенного поста")
	}
	return post.Text, post.Img1, post.Img2, post.PublishAt, nil
}

// ClearPendingPost очищает отложенный пост.
func (u *TopicUsecase) ClearPendingPost(chatID int64) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, exists := u.pendingPosts[chatID]; !exists {
		return errors.New("нет отложенного поста для очистки")
	}
	delete(u.pendingPosts, chatID)
	return nil
}

// GetScheduledPosts возвращает посты, готовые к публикации, и удаляет их из pendingPosts.
func (u *TopicUsecase) GetScheduledPosts() map[int64]struct {
	Text, Img1, Img2 string
	PublishAt        time.Time
} {
	u.mu.Lock()
	defer u.mu.Unlock()
	posts := make(map[int64]struct {
		Text, Img1, Img2 string
		PublishAt        time.Time
	})
	for chatID, post := range u.pendingPosts {
		if !post.PublishAt.IsZero() && (post.PublishAt.Before(time.Now()) || post.PublishAt.Equal(time.Now())) {
			posts[chatID] = post
			delete(u.pendingPosts, chatID) // Удаляем пост, чтобы он не публиковался повторно
		}
	}
	return posts
}

// SavePendingSchedule сохраняет состояние ожидания ввода даты.
func (u *TopicUsecase) SavePendingSchedule(chatID int64) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.pendingSchedule[chatID] = true
	return nil
}

// IsPendingSchedule проверяет, ожидается ли ввод даты.
func (u *TopicUsecase) IsPendingSchedule(chatID int64) bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.pendingSchedule[chatID]
}

// ClearPendingSchedule очищает состояние ожидания ввода даты.
func (u *TopicUsecase) ClearPendingSchedule(chatID int64) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, exists := u.pendingSchedule[chatID]; !exists {
		return errors.New("нет состояния ожидания даты")
	}
	delete(u.pendingSchedule, chatID)
	return nil
}
