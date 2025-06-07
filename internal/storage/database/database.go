package database

import (
	"codium-backend/internal/config"
	"codium-backend/internal/storage"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"os"
	"time"
)

var DB *Storage

type Storage struct {
	db  *pgxpool.Pool
	rdb *redis.Client
}

type TaskStatus struct {
	Status string `json:"status"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type SubmissionStatus struct {
	Status string `json:"status"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// SetTaskStatus устанавливает статус задачи в Redis по алиасу
func (s *Storage) SetTaskStatus(alias string, status TaskStatus) error {
	statusKey := fmt.Sprintf("task_status:%s", alias)
	statusData, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal status: %v", err)
	}
	if err := s.rdb.HSet(context.Background(), statusKey, "data", statusData).Err(); err != nil {
		return fmt.Errorf("failed to set status in Redis: %v", err)
	}
	return nil
}

// GetTaskStatus возвращает статус задачи из Redis по алиасу
func (s *Storage) GetTaskStatus(alias string) (TaskStatus, error) {
	statusKey := fmt.Sprintf("task_status:%s", alias)
	cachedStatus, err := s.rdb.HGet(context.Background(), statusKey, "data").Result()
	if err == redis.Nil {
		return TaskStatus{}, fmt.Errorf("task status not found for alias: %s", alias)
	}
	if err != nil {
		return TaskStatus{}, fmt.Errorf("failed to get status from Redis: %v", err)
	}

	var status TaskStatus
	if err := json.Unmarshal([]byte(cachedStatus), &status); err != nil {
		return TaskStatus{}, fmt.Errorf("failed to unmarshal status: %v", err)
	}
	return status, nil
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

func (s *Storage) SavePendingSubmission(taskAlias string, submissionCode []string) (int64, error) {
	query := `
        INSERT INTO submissions (task_alias, submission_code, status, hints)
        VALUES ($1, $2, 'Pending', '{}')  -- Начальное значение hints - пустой массив
        RETURNING id
    `
	var id int64
	err := s.db.QueryRow(context.Background(), query, taskAlias, submissionCode).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to save submission: %v", err)
	}
	return id, nil
}

func (s *Storage) GetSubmissionStatus(submissionID int64) (string, error) {
	query := `
        SELECT status
        FROM submissions
        WHERE id = $1
    `
	var status string
	err := s.db.QueryRow(context.Background(), query, submissionID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("submission not found")
	}
	if err != nil {
		return "", fmt.Errorf("failed to get submission status: %v", err)
	}
	return status, nil
}

// UpdateSubmissionStatusToFailed обновляет статус посылки на "Failed"
func (s *Storage) UpdateSubmissionStatusToFailed(submissionID int64) error {
	query := `
        UPDATE submissions
        SET status = 'Failed'
        WHERE id = $1
    `
	result, err := s.db.Exec(context.Background(), query, submissionID)
	if err != nil {
		return fmt.Errorf("failed to update submission status to Failed: %v", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("submission with ID %d not found", submissionID)
	}
	return nil
}

// UpdateSubmissionStatusToSuccess обновляет статус посылки на "Success"
func (s *Storage) UpdateSubmissionStatusToSuccess(submissionID int64) error {
	query := `
        UPDATE submissions
        SET status = 'Success'
        WHERE id = $1
    `
	result, err := s.db.Exec(context.Background(), query, submissionID)
	if err != nil {
		return fmt.Errorf("failed to update submission status to Success: %v", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("submission with ID %d not found", submissionID)
	}
	return nil
}

// UpdateSubmissionStatusToFailedWithHints обновляет статус посылки на "Failed" и устанавливает подсказки
func (s *Storage) UpdateSubmissionStatusToFailedWithHints(submissionID int64, hints []string) error {
	query := `
        UPDATE submissions
        SET status = 'Failed', hints = $2
        WHERE id = $1
    `
	result, err := s.db.Exec(context.Background(), query, submissionID, hints)
	if err != nil {
		return fmt.Errorf("failed to update submission status to Failed with hints: %v", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("submission with ID %d not found", submissionID)
	}
	return nil
}

// GetSubmissionHints возвращает подсказки посылки по её ID
func (s *Storage) GetSubmissionHints(submissionID int64) ([]string, error) {
	query := `
        SELECT hints
        FROM submissions
        WHERE id = $1
    `
	var hints []string
	err := s.db.QueryRow(context.Background(), query, submissionID).Scan(&hints)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("submission not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get submission hints: %v", err)
	}
	return hints, nil
}

func New(cfg *config.Config) error {
	// Чтение PostgreSQL настроек из .env
	pgUser := os.Getenv("POSTGRES_ADMIN_USER")
	pgPassword := os.Getenv("POSTGRES_ADMIN_PASSWORD")
	pgHost := os.Getenv("POSTGRES_HOST_NAME")
	pgPort := os.Getenv("POSTGRES_PORT")
	pgName := os.Getenv("POSTGRES_DB")

	if pgUser == "" || pgPassword == "" || pgHost == "" || pgPort == "" || pgName == "" {
		return fmt.Errorf("missing required PostgreSQL environment variables")
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable&timezone=UTC",
		pgUser, pgPassword, pgHost, pgPort, pgName)

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return fmt.Errorf("unable to connect to database: %v", err)
	}

	err = pool.Ping(context.Background())
	if err != nil {
		pool.Close()
		return fmt.Errorf("unable to ping database: %v", err)
	}

	// Чтение Redis настроек из .env
	redisHost := os.Getenv("REDIS_HOSTNAME")
	redisPort := os.Getenv("REDIS_PORT")
	redisPassword := os.Getenv("REDIS_PASSWORD")

	if redisHost == "" || redisPort == "" {
		pool.Close()
		return fmt.Errorf("missing required Redis environment variables")
	}

	redisAddr := fmt.Sprintf("%s:%s", redisHost, redisPort)
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       0,
	})

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		pool.Close()
		return fmt.Errorf("unable to connect to Redis: %v", err)
	}

	DB = &Storage{db: pool, rdb: rdb}
	return nil
}

func CloseDB() {
	if DB != nil {
		if DB.db != nil {
			DB.db.Close()
		}
		if DB.rdb != nil {
			err := DB.rdb.Close()
			if err != nil {
				log.Fatalf("Error closing redis db: %s", err)
				return
			}
		}
		fmt.Println("Database and Redis connections closed.")
	}
}
