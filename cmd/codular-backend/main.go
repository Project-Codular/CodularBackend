package main

import (
	_ "codular-backend/docs"
	"codular-backend/internal/config"
	"codular-backend/internal/http_server/handlers/auth"
	"codular-backend/internal/http_server/handlers/generate/noises"
	"codular-backend/internal/http_server/handlers/generate/skips"
	"codular-backend/internal/http_server/handlers/get_status/submission_status"
	"codular-backend/internal/http_server/handlers/get_status/task_status"
	"codular-backend/internal/http_server/handlers/get_task"
	"codular-backend/internal/http_server/handlers/regenerate"
	"codular-backend/internal/http_server/handlers/solve/skips_check"
	"codular-backend/internal/http_server/middleware"
	"codular-backend/internal/storage/database"
	"codular-backend/lib/logger/handlers/slogpretty"
	"fmt"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
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

// @securityDefinitions.apikey Bearer
// @in header
// @name Authorization
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

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		logger.Error("JWT_SECRET environment variable is missing")
		log.Fatal("JWT_SECRET is required")
	}

	router := chi.NewRouter()

	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://i-am-a-saw.github.io", "http://172.24.112.1:8082", "http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	router.Use(chiMiddleware.RequestID)
	router.Use(chiMiddleware.Logger)
	router.Use(chiMiddleware.Recoverer)
	router.Use(chiMiddleware.URLFormat)

	router.Get("/swagger/doc.json", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./docs/swagger.json")
	})

	router.Get("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))

	// Все маршруты под /api/v1
	router.Route("/api/v1", func(r chi.Router) {
		// Роуты без авторизации
		r.Group(func(r chi.Router) {
			r.Post("/auth/register", auth.Register(logger, storage, jwtSecret))
			r.Post("/auth/login", auth.Login(logger, storage, jwtSecret))
			r.Post("/auth/refresh", auth.Refresh(logger, storage, jwtSecret))
			r.Post("/auth/logout", auth.Logout(logger, storage))
			r.Get("/task/random", get_task.RandomTask(logger, storage))
		})

		// Роуты с авторизацией
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthMiddleware(jwtSecret, logger))
			r.Post("/skips/generate", skips.New(logger, storage, cfg))
			r.Post("/noises/generate", noises.New(logger, storage, cfg))
			r.Post("/skips/solve", skips_check.New(logger, storage))
			r.Get("/task/{alias}", get_task.New(logger, storage))
			r.Patch("/task/{alias}/regenerate", regenerate.New(logger, storage))
			r.Get("/submission-status/{submission_id}", submission_status.New(logger, storage))
			r.Get("/task-status/{alias}", task_status.GetTaskStatus(logger))
		})
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
