package regenerate

import (
	"codular-backend/internal/http_server/handlers/generate/noises"
	"codular-backend/internal/http_server/handlers/generate/skips"
	my_middleware "codular-backend/internal/http_server/middleware"
	"codular-backend/internal/storage/database"
	response_info "codular-backend/lib/api/response"
	"codular-backend/lib/logger/sl"
	"fmt"
	"github.com/go-chi/chi/v5"
	chi_middleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/go-playground/validator/v10"
	"log/slog"
	"net/http"
)

type Request struct {
	SkipsNumber *int `json:"skipsNumber,omitempty" validate:"omitempty,gte=0"`
	NoiseLevel  *int `json:"noiseLevel,omitempty" validate:"omitempty,gte=0,lte=10"`
}

type Response struct {
	ResponseInfo response_info.ResponseInfo `json:"responseInfo"`
	TaskAlias    string                     `json:"taskAlias"`
}

func getErrorResponse(msg string) *Response {
	return &Response{
		ResponseInfo: response_info.Error(msg),
		TaskAlias:    "",
	}
}

func getValidationErrorResponse(validationErrors validator.ValidationErrors) *Response {
	return &Response{
		ResponseInfo: response_info.ValidationError(validationErrors),
		TaskAlias:    "",
	}
}

func getOKResponse(taskAlias string) *Response {
	return &Response{
		ResponseInfo: response_info.OK(),
		TaskAlias:    taskAlias,
	}
}

// New regenerates an existing task by alias.
// @Summary Regenerate task by alias
// @Description Regenerates an existing task (skips or noises) by its alias with optional new parameters (skips number or noise level). Updates the task code and description in the database. Requires user authorization and edit permissions. Returns the task alias for retrieving the updated task code and description.
// @Tags Tasks
// @Accept json
// @Produce json
// @Param alias path string true "Task alias"
// @Param request body Request true "Optional skips number or noise level"
// @Success 200 {object} regenerate.Response "Successfully initiated task regeneration"
// @Success 200 {object} regenerate.Response "Example response" Example({"responseInfo":{"status":"OK"},"taskAlias":"abc123"})
// @Failure 400 {object} regenerate.Response "Invalid request, task alias is empty, or required parameters missing"
// @Failure 401 {object} regenerate.Response "Unauthorized"
// @Failure 403 {object} regenerate.Response "Forbidden: user does not have edit permissions"
// @Failure 404 {object} regenerate.Response "Task not found"
// @Failure 500 {object} regenerate.Response "Internal server error"
// @Security Bearer
// @Router /task/{alias}/regenerate [patch]
func New(logger *slog.Logger, storage *database.Storage) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		const functionPath = "internal.http_server.handlers.regenerate_task.New"

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

		// Получение деталей задачи
		taskDetails, err := storage.GetTaskDetailsByAlias(alias)
		if err != nil {
			log.Error("failed to get task details", sl.Err(err))
			writer.WriteHeader(http.StatusNotFound)
			render.JSON(writer, request, getErrorResponse("task not found"))
			return
		}

		// Проверка прав редактирования
		if taskDetails.UserID != userID {
			log.Error("user does not have edit permissions", slog.Int64("user_id", userID))
			writer.WriteHeader(http.StatusForbidden)
			render.JSON(writer, request, getErrorResponse("forbidden: user does not have edit permissions"))
			return
		}

		// Декодирование запроса
		var req Request
		if err := render.DecodeJSON(request.Body, &req); err != nil {
			log.Error("failed to decode request body", sl.Err(err))
			writer.WriteHeader(http.StatusBadRequest)
			render.JSON(writer, request, getErrorResponse("invalid request body"))
			return
		}

		// Валидация запроса
		if err := validator.New().Struct(req); err != nil {
			log.Error("invalid request", sl.Err(err))
			writer.WriteHeader(http.StatusBadRequest)
			render.JSON(writer, request, getValidationErrorResponse(err.(validator.ValidationErrors)))
			return
		}

		// Проверка параметров в зависимости от типа задачи
		if taskDetails.Type == "skips" && req.SkipsNumber == nil {
			log.Error("skipsNumber is required for skips task")
			writer.WriteHeader(http.StatusBadRequest)
			render.JSON(writer, request, getErrorResponse("skipsNumber is required for skips task"))
			return
		}
		if taskDetails.Type == "noises" && req.NoiseLevel == nil {
			log.Error("noiseLevel is required for noises task")
			writer.WriteHeader(http.StatusBadRequest)
			render.JSON(writer, request, getErrorResponse("noiseLevel is required for noises task"))
			return
		}

		// Установка начального статуса "Processing" в Redis
		initialStatus := database.TaskStatus{Status: "Processing"}
		if err := storage.SetTaskStatus(alias, initialStatus); err != nil {
			log.Error("failed to set initial status in Redis", sl.Err(err))
			writer.WriteHeader(http.StatusInternalServerError)
			render.JSON(writer, request, getErrorResponse("internal server error"))
			return
		}

		// Отправка "OK" клиенту
		writer.WriteHeader(http.StatusOK)
		render.JSON(writer, request, getOKResponse(alias))

		// Асинхронная обработка
		go processTaskAsync(log, alias, taskDetails, req, storage)
	}
}

func processTaskAsync(log *slog.Logger, alias string, taskDetails database.TaskDetails, req Request, storage *database.Storage) {
	log = log.With(slog.String("task_alias", alias), slog.Int64("user_id", taskDetails.UserID))

	var processedCode string
	var answers []string
	var err error

	description := ""
	if taskDetails.Type == "skips" {
		processedCode, answers, description, err = skips.ProcessCode(taskDetails.UserOriginalCode, *req.SkipsNumber, log)
	} else if taskDetails.Type == "noises" {
		processedCode, description, err = noises.ProcessCode(taskDetails.UserOriginalCode, *req.NoiseLevel, log)
		answers = []string{taskDetails.UserOriginalCode} // Для noises ответ — оригинальный код
	}

	if err != nil {
		// Обновление статуса на "Error" в случае ошибки
		errorStatus := database.TaskStatus{Status: "Error", Error: err.Error()}
		if err := storage.SetTaskStatus(alias, errorStatus); err != nil {
			log.Error("failed to set error status in Redis", sl.Err(err))
		}
		return
	}

	// Обновление задачи в PostgreSQL
	err = storage.UpdateTaskCodeAndAnswers(taskDetails.TaskID, processedCode, answers, description)
	if err != nil {
		// Обновление статуса на "Error" в случае ошибки сохранения
		errorStatus := database.TaskStatus{Status: "Error", Error: fmt.Sprintf("failed to update task: %v", err)}
		if err := storage.SetTaskStatus(alias, errorStatus); err != nil {
			log.Error("failed to set error status in Redis", sl.Err(err))
		}
		return
	}

	// Обновление статуса на "Done" при успехе
	doneStatus := database.TaskStatus{Status: "Done", Result: processedCode}
	if err := storage.SetTaskStatus(alias, doneStatus); err != nil {
		log.Error("failed to set done status in Redis", sl.Err(err))
	}
}
