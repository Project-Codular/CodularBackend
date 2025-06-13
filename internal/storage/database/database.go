package database

import (
	"codular-backend/internal/storage"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"os"
	"strings"
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
	Status string   `json:"status"`
	Hints  []string `json:"hints"`
	Score  int      `json:"score"`
}

type Token struct {
	UserID    int64     `json:"user_id"`
	Token     string    `json:"token"`
	Type      string    `json:"type"`
	ExpiresAt time.Time `json:"expires_at"`
}

type TaskDetails struct {
	TaskID                int64  `json:"task_id"`
	UserID                int64  `json:"user_id"`
	Type                  string `json:"type"`
	UserOriginalCode      string `json:"user_original_code"`
	ProgrammingLanguageID int64  `json:"programming_language_id"`
}

// CreateUser создаёт нового пользователя
func (s *Storage) CreateUser(email, passwordHash string) (int64, error) {
	query := `
        INSERT INTO users (email, password_hash, created_at)
        VALUES ($1, $2, $3)
        RETURNING id
    `
	var id int64
	err := s.db.QueryRow(context.Background(), query, email, passwordHash, time.Now().UTC()).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "unique constraint") {
			return 0, fmt.Errorf("email already exists")
		}
		return 0, fmt.Errorf("failed to create user: %v", err)
	}
	return id, nil
}

// GetUserByEmail возвращает пользователя по email
func (s *Storage) GetUserByEmail(email string) (int64, string, error) {
	query := `
        SELECT id, password_hash
        FROM users
        WHERE email = $1
    `
	var id int64
	var passwordHash string
	err := s.db.QueryRow(context.Background(), query, email).Scan(&id, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", fmt.Errorf("user not found")
	}
	if err != nil {
		return 0, "", fmt.Errorf("failed to get user: %v", err)
	}
	return id, passwordHash, nil
}

// SaveToken сохраняет токен в таблице tokens
func (s *Storage) SaveToken(userID int64, token, tokenType string, expiresAt time.Time) error {
	query := `
        INSERT INTO tokens (user_id, token, type, expires_at, created_at)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (token) DO UPDATE
        SET user_id = EXCLUDED.user_id, type = EXCLUDED.type, expires_at = EXCLUDED.expires_at, created_at = EXCLUDED.created_at
    `
	_, err := s.db.Exec(context.Background(), query, userID, token, tokenType, expiresAt, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to save token: %v", err)
	}
	return nil
}

// ValidateToken проверяет валидность токена
func (s *Storage) ValidateToken(token, tokenType string) (int64, bool, error) {
	query := `
        SELECT user_id, expires_at
        FROM tokens
        WHERE token = $1 AND type = $2
    `
	var userID int64
	var expiresAt time.Time
	err := s.db.QueryRow(context.Background(), query, token, tokenType).Scan(&userID, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, fmt.Errorf("token not found")
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to validate token: %v", err)
	}
	if time.Now().After(expiresAt) {
		return 0, false, fmt.Errorf("token expired")
	}
	return userID, true, nil
}

// DeleteToken удаляет токен из таблицы tokens
func (s *Storage) DeleteToken(token, tokenType string) error {
	query := `
        DELETE FROM tokens
        WHERE token = $1 AND type = $2
    `
	_, err := s.db.Exec(context.Background(), query, token, tokenType)
	if err != nil {
		return fmt.Errorf("failed to delete token: %v", err)
	}
	return nil
}

// GetTaskDetailsByAlias возвращает детали задачи по алиасу
func (s *Storage) GetTaskDetailsByAlias(alias string) (TaskDetails, error) {
	query := `
        SELECT tasks.id, tasks.user_id, tasks.type, tasks.userOriginalCode, tasks.programming_language_id
        FROM tasks
        JOIN aliases ON tasks.id = aliases.task_id
        WHERE aliases.alias = $1
    `
	var details TaskDetails
	err := s.db.QueryRow(context.Background(), query, alias).Scan(
		&details.TaskID,
		&details.UserID,
		&details.Type,
		&details.UserOriginalCode,
		&details.ProgrammingLanguageID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskDetails{}, fmt.Errorf("task not found")
	}
	if err != nil {
		return TaskDetails{}, fmt.Errorf("failed to get task details: %v", err)
	}
	return details, nil
}

// UpdateTaskCodeAndAnswers обновляет код и ответы задачи
func (s *Storage) UpdateTaskCodeAndAnswers(taskID int64, taskCode string, answers []string) error {
	query := `
        UPDATE tasks
        SET taskCode = $1, answers = $2, created_at = $3
        WHERE id = $4
    `
	result, err := s.db.Exec(context.Background(), query, taskCode, answers, time.Now().UTC(), taskID)
	if err != nil {
		return fmt.Errorf("failed to update task: %v", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("task with ID %d not found", taskID)
	}
	return nil
}

// UpdateTaskPublicStatus обновляет статус public задачи
func (s *Storage) UpdateTaskPublicStatus(taskID int64, public bool) error {
	query := `
        UPDATE tasks
        SET public = $1
        WHERE id = $2
    `
	result, err := s.db.Exec(context.Background(), query, public, taskID)
	if err != nil {
		return fmt.Errorf("failed to update task public status: %v", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("task with ID %d not found", taskID)
	}
	return nil
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

// GetRandomPublicTaskAlias возвращает случайный алиас публичной задачи
func (s *Storage) GetRandomPublicTaskAlias() (string, error) {
	query := `
        SELECT aliases.alias
        FROM aliases
        JOIN tasks ON aliases.task_id = tasks.id
        WHERE tasks.public = TRUE
        ORDER BY RANDOM()
        LIMIT 1
    `
	var alias string
	err := s.db.QueryRow(context.Background(), query).Scan(&alias)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no public tasks found")
	}
	if err != nil {
		return "", fmt.Errorf("failed to get random public task alias: %v", err)
	}
	return alias, nil
}

// SaveSkipsCodeWithAlias сохраняет код задачи с алиасом и user_id
func (s *Storage) SaveSkipsCodeWithAlias(skipsCode string, userOriginalCode string, answers []string, programmingLanguageId, userID int64, alias string) (int64, int64, error) {
	tx, err := s.db.Begin(context.Background())
	if err != nil {
		return 0, 0, fmt.Errorf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback(context.Background())

	var taskID int64
	queryTask := `
        INSERT INTO tasks (user_id, type, taskCode, userOriginalCode, answers, programming_language_id, created_at, public)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        RETURNING id
    `
	createdAt := time.Now().UTC()
	err = tx.QueryRow(context.Background(), queryTask, userID, "skips", skipsCode, userOriginalCode, answers, programmingLanguageId, createdAt, false).Scan(&taskID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to insert task: %v", err)
	}

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

	if err := tx.Commit(context.Background()); err != nil {
		return 0, 0, fmt.Errorf("failed to commit transaction: %v", err)
	}

	return taskID, aliasID, nil
}

// SaveNoisesCodeWithAlias сохраняет код задачи с алиасом и user_id
func (s *Storage) SaveNoisesCodeWithAlias(noisesCode string, userOriginalCode string, programmingLanguageId, userID int64, alias string) (int64, int64, error) {
	tx, err := s.db.Begin(context.Background())
	if err != nil {
		return 0, 0, fmt.Errorf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback(context.Background())

	var taskID int64
	queryTask := `
        INSERT INTO tasks (user_id, type, taskCode, userOriginalCode, answers, programming_language_id, created_at, public)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        RETURNING id
    `
	createdAt := time.Now().UTC()
	err = tx.QueryRow(context.Background(), queryTask, userID, "noises", noisesCode, userOriginalCode, []string{userOriginalCode}, programmingLanguageId, createdAt, false).Scan(&taskID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to insert task: %v", err)
	}

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

func (s *Storage) GetSavedTaskCode(alias string) (string, error) {
	query := `
		SELECT tasks.taskCode
		FROM aliases
		JOIN tasks ON aliases.task_id = tasks.id
		WHERE aliases.alias = $1
	`
	var savedCode string
	err := s.db.QueryRow(context.Background(), query, alias).Scan(&savedCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", storage.ErrCodeNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to get skips code: %v", err)
	}
	return savedCode, nil
}

func (s *Storage) GetCodeAnswers(codeAlias string) ([]string, error) {
	query := `
		SELECT tasks.answers
		FROM aliases
		JOIN tasks ON aliases.task_id = tasks.id
		WHERE aliases.alias = $1
	`
	var answers []string
	err := s.db.QueryRow(context.Background(), query, codeAlias).Scan(&answers)
	if errors.Is(err, pgx.ErrNoRows) {
		return []string{}, storage.ErrCodeNotFound
	}
	if err != nil {
		return []string{}, fmt.Errorf("failed to get skips code: %v", err)
	}
	return answers, nil
}

// SavePendingSubmission сохраняет новую посылку
func (s *Storage) SavePendingSubmission(taskAlias string, submissionCode []string) (int64, error) {
	query := `
        INSERT INTO submissions (task_alias, submission_code, status, hints, submitted_at)
        VALUES ($1, $2, 'Pending', NULL, $3)
        RETURNING id
    `
	var id int64
	var codeVal interface{}
	if len(submissionCode) == 0 {
		codeVal = nil
	} else {
		codeVal = submissionCode
	}
	err := s.db.QueryRow(context.Background(), query, taskAlias, codeVal, time.Now().UTC()).Scan(&id)
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

func (s *Storage) UpdateSubmissionStatusToSuccess(submissionID int64, score int) error {
	query := `
        UPDATE submissions
        SET status = 'Success', score = $2
        WHERE id = $1
    `
	result, err := s.db.Exec(context.Background(), query, submissionID, score)
	if err != nil {
		return fmt.Errorf("failed to update submission status to Success: %v", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("submission with ID %d not found", submissionID)
	}
	return nil
}

func (s *Storage) UpdateSubmissionStatusToFailedWithHints(submissionID int64, hints []string, score int) error {
	query := `
        UPDATE submissions
        SET status = 'Failed', hints = $2, score = $3
        WHERE id = $1
    `
	var hintsVal interface{}
	if len(hints) == 0 {
		hintsVal = nil
	} else {
		hintsVal = hints
	}
	result, err := s.db.Exec(context.Background(), query, submissionID, hintsVal, score)
	if err != nil {
		return fmt.Errorf("failed to update submission status to Failed with hints: %v", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("submission with ID %d not found", submissionID)
	}
	return nil
}

func (s *Storage) GetFullSubmissionStatus(submissionID int64) (SubmissionStatus, error) {
	query := `
        SELECT status, COALESCE(hints, '{}'), score
        FROM submissions
        WHERE id = $1
    `
	var status SubmissionStatus
	err := s.db.QueryRow(context.Background(), query, submissionID).Scan(&status.Status, &status.Hints, &status.Score)
	if errors.Is(err, pgx.ErrNoRows) {
		return SubmissionStatus{}, fmt.Errorf("submission not found")
	}
	if err != nil {
		return SubmissionStatus{}, fmt.Errorf("failed to get submission status and hints: %v", err)
	}
	return status, nil
}

func (s *Storage) GetSubmissionHints(submissionID int64) ([]string, error) {
	query := `
        SELECT COALESCE(hints, '{}')
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

func New() error {
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
