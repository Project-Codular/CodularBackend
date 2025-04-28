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
	"strings"
	"time"
)

var DB *pgxpool.Pool

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

func initializeDB(superUserDSN string, targetDSN string) error {
	superConn, err := pgx.Connect(context.Background(), superUserDSN)
	if err != nil {
		return fmt.Errorf("failed to connect as superuser: %v", err)
	}
	defer superConn.Close(context.Background())

	_, err = superConn.Exec(context.Background(), `
        DO
        $do$
        BEGIN
            IF NOT EXISTS (
                SELECT FROM pg_roles WHERE rolname = 'admin_codular'
            ) THEN
                CREATE ROLE admin_codular WITH LOGIN PASSWORD 'codular_password';
            END IF;
        END
        $do$
    `)
	if err != nil {
		return fmt.Errorf("failed to create user admin_codular: %v", err)
	}

	var exists bool
	err = superConn.QueryRow(context.Background(), `
        SELECT EXISTS (
            SELECT FROM pg_database WHERE datname = 'project_codular_db'
        )
    `).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if database project_codular_db exists: %v", err)
	}

	if !exists {
		_, err = superConn.Exec(context.Background(), `
            CREATE DATABASE project_codular_db
        `)
		if err != nil {
			return fmt.Errorf("failed to create database project_codular_db: %v", err)
		}
	}

	_, err = superConn.Exec(context.Background(), `
        GRANT ALL PRIVILEGES ON DATABASE project_codular_db TO admin_codular;
    `)
	if err != nil {
		return fmt.Errorf("failed to grant privileges to admin_codular: %v", err)
	}

	superUserDSNForProjectDB := strings.Replace(superUserDSN, "/postgres?", "/project_codular_db?", 1)
	projectDBConn, err := pgx.Connect(context.Background(), superUserDSNForProjectDB)
	if err != nil {
		return fmt.Errorf("failed to connect to project_codular_db as superuser: %v", err)
	}
	defer projectDBConn.Close(context.Background())

	_, err = projectDBConn.Exec(context.Background(), `
        GRANT USAGE, CREATE ON SCHEMA public TO admin_codular;
    `)
	if err != nil {
		return fmt.Errorf("failed to grant schema privileges to admin_codular: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), targetDSN)
	if err != nil {
		return fmt.Errorf("unable to connect to database: %v", err)
	}

	err = pool.Ping(context.Background())
	if err != nil {
		return fmt.Errorf("unable to ping database: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
        CREATE TABLE IF NOT EXISTS skips (
            id SERIAL PRIMARY KEY,
            user_code TEXT NOT NULL,
            skips_number INTEGER NOT NULL,
            code_alias TEXT NOT NULL,
            created_at TIMESTAMP NOT NULL
        )
    `)
	if err != nil {
		pool.Close()
		return fmt.Errorf("failed to create table skips: %v", err)
	}

	DB = pool
	return nil
}

func New(cfg *config.Config) (*Storage, error) {
	superUserDSN := "postgres://" +
		cfg.DBCredentials.SuperUser + ":" +
		cfg.DBCredentials.SuperUserPassword + "@" +
		cfg.DBCredentials.HostName + ":" +
		strconv.Itoa(cfg.DBCredentials.Port) + "/" +
		"postgres?sslmode=disable" // todo change for prod (when have SSL/TLS cert)

	dsn := "postgres://" +
		cfg.DBCredentials.User + ":" +
		cfg.DBCredentials.Password + "@" +
		cfg.DBCredentials.HostName + ":" +
		strconv.Itoa(cfg.DBCredentials.Port) + "/" +
		cfg.DBCredentials.Name + "?sslmode=disable&timezone=UTC" // todo change for prod (when have SSL/TLS cert)

	err := initializeDB(superUserDSN, dsn)
	if err != nil {
		return nil, err
	}

	return &Storage{db: DB}, nil
}

func CloseDB() {
	if DB != nil {
		DB.Close()
		fmt.Println("Database connection closed.")
	}
}
