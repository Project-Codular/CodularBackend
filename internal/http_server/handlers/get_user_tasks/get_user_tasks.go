package get_user_tasks

import (
	my_middlewre "codular-backend/internal/http_server/middleware"
	"codular-backend/internal/storage/database"
	response_info "codular-backend/lib/api/response"
	"codular-backend/lib/logger/sl"
	chi_middleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"net/http"
	"strconv"
)

type UserTasksResponse struct {
	ResponseInfo response_info.ResponseInfo `json:"responseInfo"`
	Tasks        []database.Task            `json:"tasks"`
	Total        int                        `json:"total"`
}

func getUserTasksErrorResponse(msg string) *UserTasksResponse {
	return &UserTasksResponse{
		ResponseInfo: response_info.Error(msg),
		Tasks:        []database.Task{},
		Total:        0,
	}
}

func getUserTasksOKResponse(tasks []database.Task, total int) *UserTasksResponse {
	return &UserTasksResponse{
		ResponseInfo: response_info.OK(),
		Tasks:        tasks,
		Total:        total,
	}
}

// UserTasks retrieves a paginated list of tasks for the authenticated user.
// @Summary List user tasks
// @Description Retrieves a paginated list of tasks associated with the authenticated user, sorted by creation date (descending). Requires query parameters for pagination (offset, limit). Authentication is required via Bearer token.
// @Tags Tasks
// @Produce json
// @Param offset query int true "Offset for pagination" default(0)
// @Param limit query int true "Limit for pagination" default(10)
// @Success 200 {object} task.UserTasksResponse "Successfully retrieved user task list"
// @Success 200 {object} task.UserTasksResponse "Example response" Example({"responseInfo":{"status":"OK"},"tasks":[{"alias":"xyz789","task_id":1,"type":"skips","description":"String concatenation task","programming_language":"Python","created_at":"2025-06-16T12:00:00Z"}],"total":1})
// @Failure 400 {object} task.UserTasksResponse "Invalid query parameters"
// @Failure 401 {object} task.UserTasksResponse "Unauthorized"
// @Failure 500 {object} task.UserTasksResponse "Internal server error"
// @Security Bearer
// @Router /user/tasks [get]
func UserTasks(logger *slog.Logger, storage *database.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const functionPath = "internal.http_server.handlers.task.UserTasks"

		log := logger.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", chi_middleware.GetReqID(r.Context())),
		)

		// Extract user_id from context
		userID, ok := r.Context().Value(my_middlewre.UserIDKey).(int64)
		if !ok {
			log.Error("failed to get user_id from context")
			w.WriteHeader(http.StatusUnauthorized)
			render.JSON(w, r, getUserTasksErrorResponse("unauthorized"))
			return
		}

		// Extract query parameters
		offsetStr := r.URL.Query().Get("offset")
		limitStr := r.URL.Query().Get("limit")

		// Validate and parse offset
		offset, err := strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			log.Error("invalid offset parameter", slog.String("offset", offsetStr))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getUserTasksErrorResponse("invalid offset parameter"))
			return
		}

		// Validate and parse limit
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit <= 0 {
			log.Error("invalid limit parameter", slog.String("limit", limitStr))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getUserTasksErrorResponse("invalid limit parameter"))
			return
		}

		log.Info("listing user tasks", slog.Int64("user_id", userID), slog.Int("offset", offset), slog.Int("limit", limit))

		// Fetch tasks from database
		tasks, total, err := storage.ListUserTasks(userID, offset, limit)
		if err != nil {
			log.Error("failed to list user tasks", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getUserTasksErrorResponse("failed to list user tasks"))
			return
		}

		log.Info("successfully retrieved user tasks", slog.Int("count", len(tasks)), slog.Int("total", total))
		w.WriteHeader(http.StatusOK)
		render.JSON(w, r, getUserTasksOKResponse(tasks, total))
	}
}
