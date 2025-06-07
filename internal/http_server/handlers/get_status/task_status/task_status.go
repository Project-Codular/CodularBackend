package task_status

import (
	"codium-backend/internal/storage/database"
	"codium-backend/lib/logger/sl"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"net/http"
)

type StatusResponse struct {
	Status  string `json:"status"`
	Result  string `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// GetTaskStatus возвращает статус задачи по алиасу
// @Summary Get task status
// @Description Returns the current status of a task by its alias.
// @Tags Skips
// @Produce json
// @Param alias path string true "Task alias"
// @Success 200 {object} StatusResponse "Task status retrieved successfully"
// @Success 200 {object} StatusResponse "Example response" Example({"status":"Done","result":"processed code"})
// @Failure 400 {object} StatusResponse "Alias parameter is missing"
// @Failure 404 {object} StatusResponse "Task not found"
// @Failure 500 {object} StatusResponse "Internal server error"
// @Router /task-status/{alias} [get]
func GetTaskStatus(log *slog.Logger) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		const functionPath = "internal.http_server.handlers.get_status.task_status.GetTaskStatus"

		log = log.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", middleware.GetReqID(request.Context())),
		)

		alias := chi.URLParam(request, "alias")
		if alias == "" {
			log.Error("alias parameter is missing")
			writer.WriteHeader(http.StatusBadRequest)
			render.JSON(writer, request, StatusResponse{Message: "alias parameter is required"})
			return
		}

		// Получение статуса из Redis через database.Storage
		status, err := database.DB.GetTaskStatus(alias)
		if err != nil {
			if errors.Is(err, fmt.Errorf("task status not found for alias: %s", alias)) {
				log.Error("task status not found", sl.Err(err))
				writer.WriteHeader(http.StatusNotFound)
				render.JSON(writer, request, StatusResponse{Message: "task not found"})
				return
			}
			log.Error("failed to get task status", sl.Err(err))
			writer.WriteHeader(http.StatusInternalServerError)
			render.JSON(writer, request, StatusResponse{Message: "internal server error"})
			return
		}

		// Формирование ответа
		response := StatusResponse{
			Status: status.Status,
			Result: status.Result,
			Error:  status.Error,
		}
		writer.WriteHeader(http.StatusOK)
		render.JSON(writer, request, response)

		log.Info("task status retrieved", slog.String("alias", alias), slog.String("status", status.Status))
	}
}
