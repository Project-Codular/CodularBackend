package skips

import (
	"codium-backend/internal/config"
	database "codium-backend/internal/storage/database"
	openRouterAPI "codium-backend/lib/api/openrouter"
	response_info "codium-backend/lib/api/response"
	"codium-backend/lib/logger/sl"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/go-playground/validator/v10"
	"io"
	"log"
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
	SkipsCode string   `json:"skipsCode"`
	Answers   []string `json:"answers"`
}

//go:generate go run github.com/vektra/mockery/v2@v2.53.3 --name=SkipsGenerator
type SkipsGenerator interface {
	SaveSkipsCodeWithAlias(skipsCode string, answers []string, programmingLanguageId int64, alias string) (int64, int64, error)
	GetProgrammingLanguageIDByName(name string) (int64, error)
}

//go:generate go run github.com/vektra/mockery/v2@v2.53.3 --name=TasksProvider
type TasksProvider interface {
	GetSavedCode(alias string) (string, error)
}

type AliasChecker interface {
	CheckAliasExist(alias string) (bool, error)
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

// New generates skips for the provided code and saves initial status to Redis
// @Summary Generate and save skips for code
// @Description Processes the provided source code with a specified number of skips, generates a unique alias, and saves initial status to Redis. Returns the task alias for status checking.
// @Tags Skips
// @Accept json
// @Produce json
// @Param request body Request true "Source code, number of skips, and programming language"
// @Success 200 {object} Response "Successfully initiated skips generation with task alias"
// @Success 200 {object} Response "Example response" Example({"responseInfo":{"status":"OK"},"taskAlias":"abc123"})
// @Failure 400 {object} Response "Invalid request or empty body"
// @Failure 500 {object} Response "Internal server error"
// @Router /skips/generate [post]
func New(log *slog.Logger, skipsGenerator SkipsGenerator, aliasChecker AliasChecker, cfg *config.Config) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		const functionPath = "internal.http_server.handlers.skips.New"

		log = log.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", middleware.GetReqID(request.Context())),
		)

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

		programmingLanguageId, err := skipsGenerator.GetProgrammingLanguageIDByName(decodedRequest.ProgrammingLanguage)
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
			aliasExistsInDb, err = aliasChecker.CheckAliasExist(alias)
			if err != nil {
				log.Error("failed to check alias "+alias+" existence in db", sl.Err(err))
				writer.WriteHeader(http.StatusInternalServerError)
				render.JSON(writer, request, getErrorResponse("failed to check alias existence in db"))
				return
			}
		}

		// Сохранение начального статуса "Processing" в Redis
		initialStatus := database.TaskStatus{Status: "Processing"}
		if err := database.DB.SetTaskStatus(alias, initialStatus); err != nil {
			log.Error("failed to set initial status in Redis", sl.Err(err))
			writer.WriteHeader(http.StatusInternalServerError)
			render.JSON(writer, request, getErrorResponse("internal server error"))
			return
		}

		// Отправка "OK" клиенту
		writer.WriteHeader(http.StatusOK)
		render.JSON(writer, request, getOKResponse(alias))

		// Асинхронная обработка
		go processTaskAsync(log, alias, alias, decodedRequest.Code, decodedRequest.SkipsNumber, programmingLanguageId, skipsGenerator)

		log.Info("task processing initiated", slog.String("task_alias", alias))
	}
}

// processTaskAsync асинхронно обрабатывает задачу и сохраняет результат
func processTaskAsync(log *slog.Logger, taskAlias string, alias, code string, skipsNumber int, programmingLanguageId int64, skipsGenerator SkipsGenerator) {
	log = log.With(slog.String("task_alias", taskAlias))

	processedCode, answers, err := processCode(code, skipsNumber, log)
	if err != nil {
		// Обновление статуса на "Error" в случае ошибки
		errorStatus := database.TaskStatus{Status: "Error", Error: err.Error()}
		if err := database.DB.SetTaskStatus(alias, errorStatus); err != nil {
			log.Error("failed to set error status in Redis", sl.Err(err))
		}
		return
	}

	// Сохранение в PostgreSQL
	_, _, err = skipsGenerator.SaveSkipsCodeWithAlias(processedCode, answers, programmingLanguageId, alias)
	if err != nil {
		// Обновление статуса на "Error" в случае ошибки сохранения
		errorStatus := database.TaskStatus{Status: "Error", Error: fmt.Sprintf("failed to save task: %v", err)}
		if err := database.DB.SetTaskStatus(alias, errorStatus); err != nil {
			log.Error("failed to set error status in Redis", sl.Err(err))
		}
		return
	}

	// Обновление статуса на "Done" при успехе
	doneStatus := database.TaskStatus{Status: "Done", Result: processedCode}
	if err := database.DB.SetTaskStatus(alias, doneStatus); err != nil {
		log.Error("failed to set done status in Redis", sl.Err(err))
	}
}

func processCode(code string, number int, logger *slog.Logger) (string, []string, error) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	model := "microsoft/mai-ds-r1:free"
	temperature := 0.7

	client := openRouterAPI.NewClient(apiKey, model, temperature)

	prompts, err := openRouterAPI.LoadSystemPrompts("./config/system_prompts.yaml")
	if err != nil {
		log.Fatalf("Error loading system prompts: %v", err)
	}

	systemPrompt, exists := prompts["system_prompt"]
	if !exists {
		log.Fatalf("System prompt not found in the YAML file")
	}

	response, err := client.SendChat(systemPrompt, "Число пропусков = "+strconv.Itoa(number)+"\n"+code)
	if err != nil {
		log.Fatalf("Error sending request: %v", err)
	}
	fmt.Println("Response from OpenRouter:", response)

	var decodedLLMResponse LLMResponse
	err = json.Unmarshal([]byte(response), &decodedLLMResponse)
	if err != nil {
		if errors.Is(err, io.EOF) {
			logger.Error("request body is empty")
			return "", []string{}, fmt.Errorf("request body is empty")
		} else {
			logger.Error("failed to decode request body", sl.Err(err))
			return "", []string{}, fmt.Errorf("request body is empty")
		}
	}

	logger.Info("LLM response body was decoded", slog.Any("decodedLLMResponse", decodedLLMResponse))

	return decodedLLMResponse.SkipsCode, decodedLLMResponse.Answers, nil
}

func generateAlias(length int) string {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}

	return base64.URLEncoding.EncodeToString(b)[:length]
}
