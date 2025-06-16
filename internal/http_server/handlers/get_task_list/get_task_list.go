package get_task_list

import (
	"codular-backend/internal/storage/database"
	response_info "codular-backend/lib/api/response"
	"codular-backend/lib/logger/sl"
	chi_middleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"net/http"
	"strconv"
)

type Response struct {
	ResponseInfo response_info.ResponseInfo `json:"responseInfo"`
	Tasks        []database.Task            `json:"tasks"`
	Total        int                        `json:"total"`
}

func getErrorResponse(msg string) *Response {
	return &Response{
		ResponseInfo: response_info.Error(msg),
		Tasks:        []database.Task{},
		Total:        0,
	}
}

func getOKResponse(tasks []database.Task, total int) *Response {
	return &Response{
		ResponseInfo: response_info.OK(),
		Tasks:        tasks,
		Total:        total,
	}
}

// ListTasks retrieves a paginated list of public tasks.
// @Summary List public tasks
// @Description Retrieves a paginated list of public tasks filtered by task type, sorted by creation date (descending). Requires query parameters for pagination (offset, limit) and optional task type.
// @Tags Tasks
// @Produce json
// @Param type query string false "Task type (e.g., skips, noises, or any for all types)" default(any)
// @Param offset query int true "Offset for pagination" default(0)
// @Param limit query int true "Limit for pagination" default(10)
// @Success 200 {object} task.Response "Successfully retrieved task list"
// @Success 200 {object} task.Response "Example response" Example({"responseInfo":{"status":"OK"},"tasks":[{"alias":"abc123","task_id":1,"type":"skips","description":"String concatenation task","programming_language":"Python","created_at":"2025-06-16T12:00:00Z"}],"total":1})
// @Failure 400 {object} task.Response "Invalid query parameters"
// @Failure 500 {object} task.Response "Internal server error"
// @Router /tasks [get]
func ListTasks(logger *slog.Logger, storage *database.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const functionPath = "internal.http_server.handlers.task.ListTasks"

		log := logger.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", chi_middleware.GetReqID(r.Context())),
		)

		// Extract query parameters
		taskType := r.URL.Query().Get("type")
		if taskType == "" {
			taskType = "any"
		}

		offsetStr := r.URL.Query().Get("offset")
		limitStr := r.URL.Query().Get("limit")

		// Validate and parse offset
		offset, err := strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			log.Error("invalid offset parameter", slog.String("offset", offsetStr))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getErrorResponse("invalid offset parameter"))
			return
		}

		// Validate and parse limit
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit <= 0 {
			log.Error("invalid limit parameter", slog.String("limit", limitStr))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getErrorResponse("invalid limit parameter"))
			return
		}

		log.Info("listing tasks", slog.String("type", taskType), slog.Int("offset", offset), slog.Int("limit", limit))

		// Fetch tasks from database
		tasks, total, err := storage.ListPublicTasks(taskType, offset, limit)
		if err != nil {
			log.Error("failed to list tasks", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("failed to list tasks"))
			return
		}

		log.Info("successfully retrieved tasks", slog.Int("count", len(tasks)), slog.Int("total", total))
		w.WriteHeader(http.StatusOK)
		render.JSON(w, r, getOKResponse(tasks, total))
	}
}
