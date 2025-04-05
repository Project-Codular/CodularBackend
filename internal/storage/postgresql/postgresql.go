package postgresql

import (
	"codium-backend/internal/config"
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"strconv"
)

var DB *pgxpool.Pool

func InitDB(cfg *config.Config) error {
	dsn := "postgres://" +
		cfg.DBCredentials.User + ":" +
		cfg.DBCredentials.Password + "@" +
		cfg.DBCredentials.HostName + ":" +
		strconv.Itoa(cfg.DBCredentials.Port) + "/" +
		cfg.DBCredentials.Name + "?sslmode=disable" // todo change for prod (when have SSL/TLS cert)

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return fmt.Errorf("Unable to connect to database: %v\n", err)
	}

	err = pool.Ping(context.Background())
	if err != nil {
		return fmt.Errorf("Unable to ping database: %v\n", err)
	}

	DB = pool
	return nil
}

func CloseDB() {
	if DB != nil {
		DB.Close()
		fmt.Println("Database connection closed.")
	}
}
