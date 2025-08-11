package repository

import (
	"database/sql"
	"fmt"
	"log"

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

// func TestTopicRepository_SaveAndExists(t *testing.T) {
// 	repo := NewTopicRepository(":memory:")

// 	err := repo.Save(domain.Topic{Title: "test topic"})
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	exists, err := repo.Exists("test topic")
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	if !exists {
// 		t.Fatal("expected topic to exist")
// 	}
// }
