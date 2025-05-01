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

func (s *Storage) SaveSkipsCode(userCode string, skipsNumber int, alias string) (int64, error) {
	query := `
        INSERT INTO skips (user_code, skips_number, code_alias, created_at)
        VALUES ($1, $2, $3, $4)
        RETURNING id
    `
	var id int64
	createdAt := time.Now().UTC()
	err := s.db.QueryRow(context.Background(), query, userCode, skipsNumber, alias, createdAt).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to save skips code: %v", err)
	}
	return id, nil
}

func (s *Storage) GetSkipsCode(codeAlias string) (string, error) {
	query := `
		SELECT user_code
		FROM skips
		WHERE code_alias = $1
		LIMIT 1
	`
	var userCode string
	err := s.db.QueryRow(context.Background(), query, codeAlias).Scan(&userCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", storage.ErrCodeNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to get skips code: %v", err)
	}
	return userCode, nil
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
