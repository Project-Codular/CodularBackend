package get_task

import (
	"codular-backend/internal/storage/database"
	response_info "codular-backend/lib/api/response"
	"codular-backend/lib/logger/sl"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/go-playground/validator/v10"
	"log/slog"
	"net/http"
)

type RandomTaskResponse struct {
	ResponseInfo response_info.ResponseInfo `json:"responseInfo"`
	TaskAlias    string                     `json:"taskAlias"`
}

func getErrorResponseRandomTask(msg string) *RandomTaskResponse {
	return &RandomTaskResponse{
		ResponseInfo: response_info.Error(msg),
		TaskAlias:    "",
	}
}

func getValidationErrorResponseRandomTask(validationErrors validator.ValidationErrors) *RandomTaskResponse {
	return &RandomTaskResponse{
		ResponseInfo: response_info.ValidationError(validationErrors),
		TaskAlias:    "",
	}
}

func getOKResponseRandomTask(taskAlias string) *RandomTaskResponse {
	return &RandomTaskResponse{
		ResponseInfo: response_info.OK(),
		TaskAlias:    taskAlias,
	}
}

// RandomTask redirects to a random public task.
// @Summary Get random public task
// @Description Redirects to a random public task with public = true, filtered by task type (skips, noises, or any), use this syntax: .../random?type=[type]. The redirected endpoint returns the task code and description.
// @Tags Task
// @Produce json
// @Param type query string false "Task type (skips, noises, or any)" Enums(skips, noises, any) default(any)
// @Success 302 {string} string "Redirect to /api/v1/task/{alias}"
// @Failure 400 {object} map[string]string "Invalid task type"
// @Failure 404 {object} map[string]string "No public tasks found for the specified type"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /task/random [get]
func RandomTask(log *slog.Logger, storage *database.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const functionPath = "internal.http_server.handlers.task.RandomTask"

		log = log.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		// Получение query-параметра type
		taskType := r.URL.Query().Get("type")
		if taskType == "" {
			taskType = "any" // Значение по умолчанию
		}

		// Валидация типа задачи
		if taskType != "skips" && taskType != "noises" && taskType != "any" {
			log.Error("invalid task type", slog.String("type", taskType))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getErrorResponse("invalid task type"))
			return
		}

		log.Info("processing random task request", slog.String("type", taskType))

		// Получение случайного алиаса публичной задачи
		alias, err := storage.GetRandomPublicTaskAliasByType(taskType)
		if err != nil {
			if err.Error() == "no public tasks found" {
				log.Warn("no public tasks found", slog.String("type", taskType))
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"error":"no public tasks found for the specified type"}`))
				return
			}
			log.Error("failed to get random public task", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("failed to get random public task"))
			return
		}

		log.Info("sending random public task alias", slog.String("alias", alias), slog.String("type", taskType))
		render.JSON(w, r, getOKResponseRandomTask(alias))
	}
}
