package main

import (
	_ "codium-backend/docs" // Import generated Swagger docs
	"codium-backend/internal/config"
	"codium-backend/internal/http_server/handlers/get_task"
	"codium-backend/internal/http_server/handlers/skips"
	"codium-backend/internal/http_server/handlers/task_status"
	"codium-backend/internal/storage/database"
	"codium-backend/lib/logger/handlers/slogpretty"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/swaggo/http-swagger"
	"log"
	"log/slog"
	"net/http"
	"os"
)

const (
	envLocal = "local"
	envDev   = "dev"
	envProd  = "prod"
)

// @title Codular API
// @version 1.0
// @description API for the Codular project, providing functionality to generate code skips, retrieve tasks, and check task status.
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8082
// @BasePath /api/v1
func main() {
	cfg := config.MustLoad()

	logger := setupLogger(cfg.Env)

	logger.Info("Starting Codular backend", slog.String("env", cfg.Env))
	logger.Debug("Debug messages are enabled")

	err := database.New(cfg)
	if err != nil {
		logger.Error(fmt.Sprintf("Error while initializing DB: %s", err))
		log.Fatalf("Failed to init DB: %s", err)
		return
	}
	storage := database.DB
	defer database.CloseDB()

	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.URLFormat)

	// Serve the swagger.json file
	router.Get("/swagger/doc.json", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./docs/swagger.json")
	})

	// Serve Swagger UI
	router.Get("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"), // Relative path to swagger.json
	))

	// Mount API routes under /api/v1
	router.Route("/api/v1", func(r chi.Router) {
		r.Post("/skips/generate", skips.New(logger, storage, storage, cfg))
		r.Get("/task/{alias}", get_task.New(logger, storage))
		r.Get("/task-status/{alias}", task_status.GetTaskStatus(logger))
	})

	logger.Info("starting server", slog.String("address", cfg.HTTPServer.Address))

	server := &http.Server{
		Addr:         cfg.HTTPServer.Address,
		Handler:      router,
		ReadTimeout:  cfg.HTTPServer.Timeout,
		WriteTimeout: cfg.HTTPServer.Timeout,
		IdleTimeout:  cfg.HTTPServer.IdleTimeout,
	}

	if err := server.ListenAndServe(); err != nil {
		logger.Error("failed to start server")
	}

	logger.Error("server stopped")
}

func setupLogger(env string) *slog.Logger {
	var logger *slog.Logger
	switch env {
	case envLocal:
		logger = setupPrettySlog(slog.LevelDebug)
	case envDev:
		logger = slog.New(
			slog.NewJSONHandler(
				os.Stdout,
				&slog.HandlerOptions{
					Level: slog.LevelDebug,
				},
			),
		)
	case envProd:
		logger = slog.New(
			slog.NewJSONHandler(
				os.Stdout,
				&slog.HandlerOptions{
					Level: slog.LevelInfo,
				},
			),
		)
	}

	return logger
}

func setupPrettySlog(level slog.Level) *slog.Logger {
	opts := slogpretty.PrettyHandlerOptions{
		SlogOpts: &slog.HandlerOptions{
			Level: level,
		},
	}

	handler := opts.NewPrettyHandler(os.Stdout)

	return slog.New(handler)
}
