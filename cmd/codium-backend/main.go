package main

import (
	"codium-backend/internal/config"
	"log/slog"
)

const (
	envLocal = "local"
	envDev   = "dev"
	envProd  = "prod"
)

func main() {
	cfg := config.MustLoad()

	// setup logger

	// init storage

	// init router

	// start server
}
