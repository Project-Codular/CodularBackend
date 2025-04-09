package postgresql

import (
	"codium-backend/internal/config"
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"strconv"
)

var DB *pgxpool.Pool

type Storage struct {
	db *pgxpool.Pool
}

func (s *Storage) SaveSkipsCode(userCode string, skipsNumber int) (int64, error) {
	//TODO implement me
	panic("implement me")
}

func (s *Storage) GetSkipsCode(codeAlias string) (string, error) {
	//TODO implement me
	panic("implement me")
}

func New(cfg *config.Config) (*Storage, error) {
	dsn := "postgres://" +
		cfg.DBCredentials.User + ":" +
		cfg.DBCredentials.Password + "@" +
		cfg.DBCredentials.HostName + ":" +
		strconv.Itoa(cfg.DBCredentials.Port) + "/" +
		cfg.DBCredentials.Name + "?sslmode=disable" // todo change for prod (when have SSL/TLS cert)

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("Unable to connect to database: %v\n", err)
	}

	err = pool.Ping(context.Background())
	if err != nil {
		return nil, fmt.Errorf("Unable to ping database: %v\n", err)
	}

	DB = pool
	return &Storage{db: DB}, nil
}

func CloseDB() {
	if DB != nil {
		DB.Close()
		fmt.Println("Database connection closed.")
	}
}
