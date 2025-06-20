package skips

import (
	"codular-backend/internal/config"
	my_middleware "codular-backend/internal/http_server/middleware"
	database "codular-backend/internal/storage/database"
	openRouterAPI "codular-backend/lib/api/openrouter"
	response_info "codular-backend/lib/api/response"
	"codular-backend/lib/logger/sl"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	chi_middleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/go-playground/validator/v10"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
)

type Request struct {
	Code                string `json:"sourceCode" validate:"required"`
	SkipsNumber         int    `json:"skipsNumber" validate:"required,gte=0"`
	ProgrammingLanguage string `json:"programmingLanguage" validate:"required"`
}

type Response struct {
	ResponseInfo response_info.ResponseInfo `json:"responseInfo"`
	TaskAlias    string                     `json:"taskAlias"`
}

type LLMResponse struct {
	Description string   `json:"description"`
	SkipsCode   string   `json:"skipsCode"`
	Answers     []string `json:"answers"`
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

// New generates skips for the provided code and saves it to the database.
// @Summary Generate and save skips code
// @Description Processes the provided source code with a specified number of skips, generates a unique alias, saves the task with its description to the database, and initiates asynchronous processing. Returns the task alias for retrieving the task code and description.
// @Tags Skips
// @Accept json
// @Produce json
// @Param request body Request true "Source code, number of skips, and programming language"
// @Success 200 {object} skips.Response "Successfully initiated skips generation"
// @Success 200 {object} skips.Response "Example response" Example({"responseInfo":{"status":"OK"},"taskAlias":"abc123"})
// @Failure 400 {object} skips.Response "Invalid request, empty body, or invalid programming language"
// @Failure 401 {object} skips.Response "Unauthorized"
// @Failure 500 {object} skips.Response "Internal server error"
// @Security Bearer
// @Router /skips/generate [post]
func New(log *slog.Logger, storage *database.Storage, cfg *config.Config) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		const functionPath = "internal.http_server.handlers.generate.skips.New"

		log = log.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", chi_middleware.GetReqID(request.Context())),
		)

		// Извлечение user_id из контекста
		userID, ok := request.Context().Value(my_middleware.UserIDKey).(int64)
		if !ok {
			log.Error("failed to get user_id from context")
			writer.WriteHeader(http.StatusUnauthorized)
			render.JSON(writer, request, getErrorResponse("unauthorized"))
			return
		}

		var decodedRequest Request
		err := render.DecodeJSON(request.Body, &decodedRequest)
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Error("request body is empty")
				writer.WriteHeader(http.StatusBadRequest)
				render.JSON(writer, request, getErrorResponse("empty request"))
				return
			} else {
				log.Error("failed to decode request body", sl.Err(err))
				writer.WriteHeader(http.StatusInternalServerError)
				render.JSON(writer, request, getErrorResponse("failed to decode request"))
				return
			}
		}

		log.Info("request body was decoded", slog.Any("decodedRequest", decodedRequest))

		if err := validator.New().Struct(decodedRequest); err != nil {
			var validationErrs validator.ValidationErrors
			if errors.As(err, &validationErrs) {
				log.Error("invalid request", sl.Err(err))
				writer.WriteHeader(http.StatusBadRequest)
				render.JSON(writer, request, getValidationErrorResponse(validationErrs))
				return
			}
		}

		programmingLanguageId, err := storage.GetProgrammingLanguageIDByName(decodedRequest.ProgrammingLanguage)
		if err != nil {
			log.Error("invalid programming language: "+decodedRequest.ProgrammingLanguage, sl.Err(err))
			writer.WriteHeader(http.StatusBadRequest)
			render.JSON(writer, request, getErrorResponse("invalid programming language: "+decodedRequest.ProgrammingLanguage))
			return
		}

		// Генерация уникального алиаса
		aliasExistsInDb := true
		var alias string
		for aliasExistsInDb {
			alias = generateAlias(cfg.AliasLength)
			aliasExistsInDb, err = storage.CheckAliasExist(alias)
			if err != nil {
				log.Error("failed to check alias "+alias+" existence in db", sl.Err(err))
				writer.WriteHeader(http.StatusInternalServerError)
				render.JSON(writer, request, getErrorResponse("failed to check alias existence in db"))
				return
			}
		}

		// Сохранение начального статуса "Processing" в Redis
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
		go processTaskAsync(log, alias, decodedRequest.Code, decodedRequest.SkipsNumber, programmingLanguageId, userID, storage)

		log.Info("task processing initiated", slog.String("task_alias", alias), slog.Int64("user_id", userID))
	}
}

// processTaskAsync асинхронно обрабатывает задачу и сохраняет результат
func processTaskAsync(log *slog.Logger, alias, code string, skipsNumber int, programmingLanguageId, userID int64, storage *database.Storage) {
	log = log.With(slog.String("task_alias", alias), slog.Int64("user_id", userID))

	processedCode, answers, description, err := ProcessCode(code, skipsNumber, log)
	if err != nil {
		// Обновление статуса на "Error" в случае ошибки
		errorStatus := database.TaskStatus{Status: "Error", Error: err.Error()}
		if err := storage.SetTaskStatus(alias, errorStatus); err != nil {
			log.Error("failed to set error status in Redis", sl.Err(err))
		}
		return
	}

	// Сохранение в PostgreSQL
	_, _, err = storage.SaveSkipsCodeWithAlias(processedCode, code, answers, programmingLanguageId, userID, alias, description)
	if err != nil {
		// Обновление статуса на "Error" в случае ошибки сохранения
		errorStatus := database.TaskStatus{Status: "Error", Error: fmt.Sprintf("failed to save task: %v", err)}
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

func ProcessCode(code string, number int, logger *slog.Logger) (string, []string, string, error) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	model := os.Getenv("MODEL")
	temperature := 0.7

	client := openRouterAPI.NewClient(apiKey, model, temperature)

	prompts, err := openRouterAPI.LoadSystemPrompts("./config/system_prompts.yaml")
	if err != nil {
		logger.Error("failed to load system prompts", sl.Err(err))
		return "", []string{}, "", fmt.Errorf("failed to load system prompts: %v", err)
	}

	systemPrompt, exists := prompts["system_prompt"]
	if !exists {
		logger.Error("system prompt not found in YAML file")
		return "", []string{}, "", fmt.Errorf("system prompt not found")
	}

	response, err := client.SendChat(systemPrompt, "Число пропусков = "+strconv.Itoa(number)+"\n"+code)
	if err != nil {
		logger.Error("failed to send request to OpenRouter", sl.Err(err))
		return "", []string{}, "", fmt.Errorf("failed to send request: %v", err)
	}
	fmt.Println("Response from OpenRouter:", response)

	var decodedLLMResponse LLMResponse
	cleanedResponse := openRouterAPI.CleanLLMResponse(response)
	fmt.Println("Cleaned response from OpenRouter:", cleanedResponse)
	err = json.Unmarshal([]byte(cleanedResponse), &decodedLLMResponse)
	if err != nil {
		if errors.Is(err, io.EOF) {
			logger.Error("request body is empty")
			return "", []string{}, "", fmt.Errorf("request body is empty")
		} else {
			logger.Error("failed to decode request body", sl.Err(err))
			return "", []string{}, "", fmt.Errorf("failed to decode request: %v", err)
		}
	}

	logger.Info("LLM response body was decoded", slog.Any("decodedLLMResponse", decodedLLMResponse))

	return decodedLLMResponse.SkipsCode, decodedLLMResponse.Answers, decodedLLMResponse.Description, nil
}

func generateAlias(length int) string {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}

	return base64.URLEncoding.EncodeToString(b)[:length]
}
