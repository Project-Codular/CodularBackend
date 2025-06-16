package edit_task

import (
	myMiddleware "codular-backend/internal/http_server/middleware"
	"codular-backend/internal/storage/database"
	response_info "codular-backend/lib/api/response"
	"codular-backend/lib/logger/sl"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/go-playground/validator/v10"
	"log/slog"
	"net/http"
)

type SetPublicRequest struct {
	Public *bool `json:"public" validate:"required"`
}

type SetPublicResponse struct {
	ResponseInfo response_info.ResponseInfo `json:"response_info"`
	TaskAlias    string                     `json:"taskAlias"`
}

func getErrorResponse(msg string) *SetPublicResponse {
	return &SetPublicResponse{
		ResponseInfo: response_info.Error(msg),
		TaskAlias:    "",
	}
}

func getValidationErrorResponse(validationErrors validator.ValidationErrors) *SetPublicResponse {
	return &SetPublicResponse{
		ResponseInfo: response_info.ValidationError(validationErrors),
		TaskAlias:    "",
	}
}

func getOKResponse(taskAlias string) *SetPublicResponse {
	return &SetPublicResponse{
		ResponseInfo: response_info.OK(),
		TaskAlias:    taskAlias,
	}
}

// ChangeAccess изменяет статус public для задачи по алиасу
// @Summary Set task public status
// @Description Updates the public status of a task identified by its alias. Requires user authorization and edit permissions.
// @Tags Task
// @Accept json
// @Produce json
// @Param alias path string true "Task alias"
// @Param request body SetPublicRequest true "Public status"
// @Success 200 {object} SetPublicResponse "Successfully updated task public status"
// @Success 200 {object} SetPublicResponse "Example response" Example({"response_info":{"status":"OK"},"taskAlias":"abc123"})
// @Failure 400 {object} SetPublicResponse "Invalid request or task alias is empty"
// @Failure 401 {object} SetPublicResponse "Unauthorized"
// @Failure 403 {object} SetPublicResponse "Forbidden: user does not have edit permissions"
// @Failure 404 {object} SetPublicResponse "Task not found"
// @Failure 500 {object} SetPublicResponse "Internal server error"
// @Router /task/{alias}/set-public [patch]
func ChangeAccess(log *slog.Logger, storage *database.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const functionPath = "internal.http_server.handlers.task.ChangeAccess"

		log = log.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", chiMiddleware.GetReqID(r.Context())),
		)

		alias := chi.URLParam(r, "alias")
		if alias == "" {
			log.Error("task alias is empty")
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getErrorResponse("task alias is empty"))
			return
		}

		log.Info("got alias from request path", slog.String("alias", alias))

		// Извлечение user_id из контекста
		userID, ok := r.Context().Value(myMiddleware.UserIDKey).(int64)
		if !ok {
			log.Error("failed to get user_id from context")
			w.WriteHeader(http.StatusUnauthorized)
			render.JSON(w, r, getErrorResponse("unauthorized"))
			return
		}

		// Получение деталей задачи
		taskDetails, err := storage.GetTaskDetailsByAlias(alias)
		if err != nil {
			log.Error("failed to get task details", sl.Err(err))
			w.WriteHeader(http.StatusNotFound)
			render.JSON(w, r, getErrorResponse("task not found"))
			return
		}

		// Проверка прав редактирования
		if taskDetails.UserID != userID {
			log.Error("user does not have edit permissions", slog.Int64("user_id", userID))
			w.WriteHeader(http.StatusForbidden)
			render.JSON(w, r, getErrorResponse("forbidden: user does not have edit permissions"))
			return
		}

		// Декодирование запроса
		var req SetPublicRequest
		if err := render.DecodeJSON(r.Body, &req); err != nil {
			log.Error("failed to decode request body", sl.Err(err))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getErrorResponse("invalid request body"))
			return
		}

		// Валидация запроса
		if err := validator.New().Struct(req); err != nil {
			log.Error("invalid request", sl.Err(err))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getValidationErrorResponse(err.(validator.ValidationErrors)))
			return
		}

		// Обновление статуса public
		if err := storage.UpdateTaskPublicStatus(taskDetails.TaskID, *req.Public); err != nil {
			log.Error("failed to update task public status", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		log.Info("task public status updated", slog.String("alias", alias), slog.Bool("public", *req.Public))
		w.WriteHeader(http.StatusOK)
		render.JSON(w, r, getOKResponse(alias))
	}
}
