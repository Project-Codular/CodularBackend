package postgresql

import (
	"codium-backend/internal/config"
	"codium-backend/internal/storage"
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strconv"
	"time"
)

var DB *Storage

type Storage struct {
	db *pgxpool.Pool
}

func (s *Storage) SaveSkipsCodeWithAlias(skipsCode string, answers []string, programmingLanguageId int64, alias string) (int64, int64, error) {
	tx, err := s.db.Begin(context.Background())
	if err != nil {
		return 0, 0, fmt.Errorf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback(context.Background())

	// Вставка задачи
	var taskID int64
	queryTask := `
        INSERT INTO tasks (type, taskCode, answers, programming_language_id, created_at)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING id
    `
	createdAt := time.Now().UTC()
	err = tx.QueryRow(context.Background(), queryTask, "skips", skipsCode, answers, programmingLanguageId, createdAt).Scan(&taskID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to insert task: %v", err)
	}

	// Вставка алиаса
	var aliasID int64
	queryAlias := `
		INSERT INTO aliases (alias, task_id) 
		VALUES ($1, $2)
		RETURNING id
	`
	err = tx.QueryRow(context.Background(), queryAlias, alias, taskID).Scan(&aliasID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to insert alias: %v", err)
	}

	// Фиксация транзакции
	if err := tx.Commit(context.Background()); err != nil {
		return 0, 0, fmt.Errorf("failed to commit transaction: %v", err)
	}

	return taskID, aliasID, nil
}

func (s *Storage) GetProgrammingLanguageIDByName(name string) (int64, error) {
	query := `
        SELECT id
        FROM programming_languages
        WHERE name = $1
    `
	var id int64
	err := s.db.QueryRow(context.Background(), query, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("programming language %q not found", name)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get programming language ID: %v", err)
	}
	return id, nil
}

func (s *Storage) CheckAliasExist(alias string) (bool, error) {
	query := `
        SELECT EXISTS (
            SELECT 1
            FROM aliases
            WHERE alias = $1
        )
    `
	var exists bool
	err := s.db.QueryRow(context.Background(), query, alias).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check alias existence: %v", err)
	}
	return exists, nil
}

func (s *Storage) SaveAlias(alias string, taskId int64) (int64, error) {
	query := `
		INSERT INTO aliases (alias, task_id) 
		VALUES ($1, $2)
		RETURNING id
	`
	var id int64
	err := s.db.QueryRow(context.Background(), query, alias, taskId).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to save alias: %v", err)
	}
	return id, nil
}

func (s *Storage) GetSavedCode(alias string) (string, error) {
	query := `
		SELECT tasks.taskCode
		FROM aliases
		JOIN tasks ON aliases.task_id = tasks.id
		WHERE aliases.alias = $1;
	`
	var savedCode string
	err := s.db.QueryRow(context.Background(), query, alias).Scan(&savedCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", storage.ErrCodeNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to get_task skips code: %v", err)
	}
	return savedCode, nil
}

func (s *Storage) GetCodeAnswers(codeAlias string) ([]string, error) {
	query := `
		SELECT tasks.answers
		FROM aliases
		JOIN tasks ON aliases.task_id = tasks.id
		WHERE aliases.alias = $1;
	`
	var answers []string
	err := s.db.QueryRow(context.Background(), query, codeAlias).Scan(&answers)
	if errors.Is(err, pgx.ErrNoRows) {
		return []string{}, storage.ErrCodeNotFound
	}
	if err != nil {
		return []string{}, fmt.Errorf("failed to get_task skips code: %v", err)
	}
	return answers, nil
}

func New(cfg *config.Config) error {
	dsn := "postgres://" +
		cfg.DBCredentials.User + ":" +
		cfg.DBCredentials.Password + "@" +
		cfg.DBCredentials.HostName + ":" +
		strconv.Itoa(cfg.DBCredentials.Port) + "/" +
		cfg.DBCredentials.Name + "?sslmode=disable&timezone=UTC" // todo change for prod (when have SSL/TLS cert)

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return fmt.Errorf("unable to connect to database: %v", err)
	}

	err = pool.Ping(context.Background())
	if err != nil {
		pool.Close()
		return fmt.Errorf("unable to ping database: %v", err)
	}

	DB = &Storage{db: pool}
	return nil
}

func CloseDB() {
	if DB != nil {
		DB.db.Close()
		fmt.Println("Database connection closed.")
	}
}
