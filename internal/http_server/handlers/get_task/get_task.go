package get_task

import (
	my_middleware "codular-backend/internal/http_server/middleware"
	"codular-backend/internal/storage/database"
	response_info "codular-backend/lib/api/response"
	"codular-backend/lib/logger/sl"
	"github.com/go-chi/chi/v5"
	chi_middleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"net/http"
)

type Response struct {
	ResponseInfo response_info.ResponseInfo `json:"responseInfo"`
	CodeToSolve  string                     `json:"codeToSolve"`
	CanEdit      bool                       `json:"canEdit"`
}

func getErrorResponse(msg string) *Response {
	return &Response{
		ResponseInfo: response_info.Error(msg),
		CodeToSolve:  "",
		CanEdit:      false,
	}
}

func getOKResponse(codeToSolve string, canEdit bool) *Response {
	return &Response{
		ResponseInfo: response_info.OK(),
		CodeToSolve:  codeToSolve,
		CanEdit:      canEdit,
	}
}

// New retrieves a task by alias
// @Summary Get task by alias
// @Description Retrieves the processed code associated with the given alias from the database, including whether the user can edit the task.
// @Tags Tasks
// @Produce json
// @Param alias path string true "Task alias"
// @Success 200 {object} Response "Successfully retrieved task"
// @Success 200 {object} Response "Example response" Example({"responseInfo":{"status":"OK"},"codeToSolve":"processed code","canEdit":true})
// @Failure 400 {object} Response "Task alias is empty"
// @Failure 401 {object} Response "Unauthorized"
// @Failure 404 {object} Response "Task not found"
// @Failure 500 {object} Response "Internal server error"
// @Router /task/{alias} [get]
func New(logger *slog.Logger, storage *database.Storage) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		const functionPath = "internal.http_server.handlers.get_task.New"

		log := logger.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", chi_middleware.GetReqID(request.Context())),
		)

		alias := chi.URLParam(request, "alias")
		if alias == "" {
			log.Error("task alias is empty")
			writer.WriteHeader(http.StatusBadRequest)
			render.JSON(writer, request, getErrorResponse("task alias is empty"))
			return
		}

		log.Info("got alias from request path", slog.String("alias", alias))

		// Извлечение user_id из контекста
		userID, ok := request.Context().Value(my_middleware.UserIDKey).(int64)
		if !ok {
			log.Error("failed to get user_id from context")
			writer.WriteHeader(http.StatusUnauthorized)
			render.JSON(writer, request, getErrorResponse("unauthorized"))
			return
		}

		// Получение кода задачи
		codeFromDb, err := storage.GetSavedTaskCode(alias)
		if err != nil {
			log.Error("failed to get code", sl.Err(err))
			writer.WriteHeader(http.StatusNotFound)
			render.JSON(writer, request, getErrorResponse("task not found"))
			return
		}

		// Получение user_id задачи
		taskDetails, err := storage.GetTaskDetailsByAlias(alias)
		if err != nil {
			log.Error("failed to get task user_id", sl.Err(err))
			writer.WriteHeader(http.StatusNotFound)
			render.JSON(writer, request, getErrorResponse("task not found"))
			return
		}

		// Проверка прав редактирования
		canEdit := userID == taskDetails.UserID

		log.Info("got task by alias from db", slog.String("alias", alias), slog.Bool("canEdit", canEdit))
		writer.WriteHeader(http.StatusOK)
		render.JSON(writer, request, getOKResponse(codeFromDb, canEdit))
	}
}
