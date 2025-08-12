package repository

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	// "testing"

	"lady/internal/domain"

	_ "modernc.org/sqlite"
)

type TopicRepository struct {
	db *sql.DB
}

func NewTopicRepository(dbPath string) *TopicRepository {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS topics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT
	)`)
	if err != nil {
		log.Fatal(err)
	}

	return &TopicRepository{db: db}
}

func (r *TopicRepository) Exists(title string) (bool, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM topics WHERE title = ?", title).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *TopicRepository) Save(topic domain.Topic) error {
	exists, err := r.Exists(topic.Title)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("тема уже существует")
	}

	res, err := r.db.Exec("INSERT INTO topics (title) VALUES (?)", topic.Title)
	if err != nil {
		log.Printf("Ошибка вставки темы в БД: %v", err)
		return err
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		log.Printf("Вставка темы не изменила строки")
	}

	return nil
}

func (r *TopicRepository) List() ([]domain.Topic, error) {
	rows, err := r.db.Query("SELECT id, title FROM topics ORDER BY id DESC LIMIT 50")
	if err != nil {
		log.Printf("DB Query error: %v", err)
		return nil, err
	}
	defer rows.Close()

	var topics []domain.Topic
	for rows.Next() {
		var t domain.Topic
		if err := rows.Scan(&t.ID, &t.Title); err != nil {
			log.Printf("Row scan error: %v", err)
			return nil, err
		}
		topics = append(topics, t)
	}
	log.Printf("DB returned %d rows", len(topics))
	return topics, nil
}

func (r *TopicRepository) SavePendingPost(chatID int64, text, img1, img2 string, publishAt time.Time) error {
	var publishAtVal interface{}
	if !publishAt.IsZero() {
		publishAtVal = publishAt.Format("2006-01-02 15:04:05")
	} else {
		publishAtVal = nil // Сохраняем NULL вместо пустой строки
	}
	res, err := r.db.Exec(
		`INSERT OR REPLACE INTO pending_posts (chat_id, text, img1, img2, publish_at) VALUES (?, ?, ?, ?, ?)`,
		chatID, text, img1, img2, publishAtVal,
	)
	if err != nil {
		log.Printf("Ошибка сохранения отложенного поста для chatID %d: %v", chatID, err)
		return err
	}
	affected, _ := res.RowsAffected()
	log.Printf("Сохранен отложенный пост для chatID %d, затронуто строк: %d, publish_at: %v", chatID, affected, publishAtVal)
	return nil
}

func (r *TopicRepository) GetPendingPost(chatID int64) (string, string, string, time.Time, error) {
	var text, img1, img2 string
	var publishAtStr sql.NullString
	err := r.db.QueryRow(
		`SELECT text, img1, img2, publish_at FROM pending_posts WHERE chat_id = ?`,
		chatID,
	).Scan(&text, &img1, &img2, &publishAtStr)
	if err == sql.ErrNoRows {
		return "", "", "", time.Time{}, fmt.Errorf("no pending post for chatID %d", chatID)
	}
	if err != nil {
		log.Printf("Ошибка получения отложенного поста для chatID %d: %v", chatID, err)
		return "", "", "", time.Time{}, err
	}

	var publishAt time.Time
	if publishAtStr.Valid && publishAtStr.String != "" {
		loc, err := time.LoadLocation("Asia/Novosibirsk")
		if err != nil {
			log.Printf("Ошибка загрузки часового пояса Asia/Novosibirsk: %v", err)
			return "", "", "", time.Time{}, err
		}
		publishAt, err = time.ParseInLocation("2006-01-02 15:04:05", publishAtStr.String, loc)
		if err != nil {
			log.Printf("Ошибка парсинга времени publish_at для chatID %d: %v", chatID, err)
			return "", "", "", time.Time{}, err
		}
	}

	return text, img1, img2, publishAt, nil
}
func (r *TopicRepository) ClearPendingPost(chatID int64) error {
	res, err := r.db.Exec(`DELETE FROM pending_posts WHERE chat_id = ?`, chatID)
	if err != nil {
		log.Printf("Ошибка очистки отложенного поста для chatID %d: %v", chatID, err)
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("no pending post for chatID %d", chatID)
	}
	log.Printf("Очищен отложенный пост для chatID %d", chatID)
	return nil
}
func (r *TopicRepository) GetScheduledPosts() map[int64]struct {
	Text      string
	Img1      string
	Img2      string
	PublishAt time.Time
} {
	result := make(map[int64]struct {
		Text      string
		Img1      string
		Img2      string
		PublishAt time.Time
	})

	loc, err := time.LoadLocation("Asia/Novosibirsk")
	if err != nil {
		log.Printf("Ошибка загрузки часового пояса Asia/Novosibirsk: %v", err)
		return result
	}
	currentTime := time.Now().In(loc).Format("2006-01-02 15:04:05")
	log.Printf("Запрос отложенных постов с publish_at <= %s", currentTime)

	rows, err := r.db.Query(
		`SELECT chat_id, text, img1, img2, publish_at FROM pending_posts WHERE publish_at IS NOT NULL AND publish_at != '' AND publish_at <= ?`,
		currentTime,
	)
	if err != nil {
		log.Printf("Ошибка запроса отложенных постов: %v", err)
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var chatID int64
		var text, img1, img2 string
		var publishAtStr sql.NullString
		if err := rows.Scan(&chatID, &text, &img1, &img2, &publishAtStr); err != nil {
			log.Printf("Ошибка чтения строки отложенного поста: %v", err)
			continue
		}
		if !publishAtStr.Valid || publishAtStr.String == "" {
			log.Printf("Пропущена запись для chatID %d: publish_at пустое или NULL", chatID)
			continue
		}
		publishAt, err := time.ParseInLocation("2006-01-02 15:04:05", publishAtStr.String, loc)
		if err != nil {
			log.Printf("Ошибка парсинга времени publish_at для chatID %d: %v", chatID, err)
			continue
		}
		result[chatID] = struct {
			Text      string
			Img1      string
			Img2      string
			PublishAt time.Time
		}{Text: text, Img1: img1, Img2: img2, PublishAt: publishAt}
		log.Printf("Найден отложенный пост для chatID %d, запланирован на %s", chatID, publishAt.Format("02.01.2006 15:04:05"))
	}
	log.Printf("Найдено %d запланированных постов", len(result))
	return result
}

func (r *TopicRepository) ListScheduledPosts() map[int64]struct {
	Text      string
	Img1      string
	Img2      string
	PublishAt time.Time
} {
	result := make(map[int64]struct {
		Text      string
		Img1      string
		Img2      string
		PublishAt time.Time
	})

	loc, err := time.LoadLocation("Asia/Novosibirsk")
	if err != nil {
		log.Printf("Ошибка загрузки часового пояса Asia/Novosibirsk: %v", err)
		return result
	}
	log.Printf("Запрос всех запланированных постов")

	rows, err := r.db.Query(
		`SELECT chat_id, text, img1, img2, publish_at FROM pending_posts WHERE publish_at IS NOT NULL AND publish_at != ''`,
	)
	if err != nil {
		log.Printf("Ошибка запроса всех запланированных постов: %v", err)
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var chatID int64
		var text, img1, img2 string
		var publishAtStr sql.NullString
		if err := rows.Scan(&chatID, &text, &img1, &img2, &publishAtStr); err != nil {
			log.Printf("Ошибка чтения строки запланированного поста: %v", err)
			continue
		}
		if !publishAtStr.Valid || publishAtStr.String == "" {
			log.Printf("Пропущена запись для chatID %d: publish_at пустое или NULL", chatID)
			continue
		}
		publishAt, err := time.ParseInLocation("2006-01-02 15:04:05", publishAtStr.String, loc)
		if err != nil {
			log.Printf("Ошибка парсинга времени publish_at для chatID %d: %v", chatID, err)
			continue
		}
		result[chatID] = struct {
			Text      string
			Img1      string
			Img2      string
			PublishAt time.Time
		}{Text: text, Img1: img1, Img2: img2, PublishAt: publishAt}
		log.Printf("Найден запланированный пост для chatID %d, запланирован на %s", chatID, publishAt.Format("02.01.2006 15:04:05"))
	}
	log.Printf("Найдено %d запланированных постов (все)", len(result))
	return result
}
