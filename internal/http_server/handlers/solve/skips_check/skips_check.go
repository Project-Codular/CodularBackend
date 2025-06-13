package skips_check

import (
	"codular-backend/internal/storage/database"
	openRouterAPI "codular-backend/lib/api/openrouter"
	response_info "codular-backend/lib/api/response"
	"codular-backend/lib/logger/sl"
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

type ClientRequest struct {
	TaskAlias string   `json:"taskAlias" validate:"required"`
	Answers   []string `json:"answers" validate:"required"`
}

type ServerResponse struct {
	ResponseInfo response_info.ResponseInfo `json:"responseInfo"`
	SubmissionID int64                      `json:"submissionId"`
}

type LLMResponse struct {
	Status string  `json:"status"`
	Hints  []Hints `json:"hints"`
}

type Hints struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

type AliasChecker interface {
	CheckAliasExist(alias string) (bool, error)
}

type SubmissionStorage interface {
	SavePendingSubmission(taskAlias string, submissionAnswers []string) (int64, error)
	UpdateSubmissionStatusToFailed(submissionID int64) error
	UpdateSubmissionStatusToFailedWithHints(submissionID int64, hints []string) error
	UpdateSubmissionStatusToSuccess(submissionID int64) error
}

type TasksStorage interface {
	GetCodeAnswers(codeAlias string) ([]string, error)
	GetSavedCode(alias string) (string, error)
}

func getErrorResponse(msg string) *ServerResponse {
	return &ServerResponse{
		ResponseInfo: response_info.Error(msg),
		SubmissionID: -1,
	}
}

func getValidationErrorResponse(validationErrors validator.ValidationErrors) *ServerResponse {
	return &ServerResponse{
		ResponseInfo: response_info.ValidationError(validationErrors),
		SubmissionID: -1,
	}
}

func getOKResponse(submissionID int64) *ServerResponse {
	return &ServerResponse{
		ResponseInfo: response_info.OK(),
		SubmissionID: submissionID,
	}
}

// New handles the submission of answers for a skips task.
// @Summary Submit answers for a skips task
// @Description Receives user answers for a given task alias, saves the submission, and asynchronously processes it.
// @Tags Skips
// @Accept json
// @Produce json
// @Param request body ClientRequest true "Task alias and user's answers"
// @Success 200 {object} ServerResponse "Successfully initiated submission processing"
// @Success 200 {object} ServerResponse "Example response for successful submission" Example({"responseInfo":{"status":"OK"},"submissionId":123})
// @Failure 400 {object} ServerResponse "Invalid request body or validation error"
// @Failure 404 {object} ServerResponse "Task not found"
// @Failure 500 {object} ServerResponse "Internal server error"
// @Router /skips/solve [post]
func New(log *slog.Logger, storage *database.Storage) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		const functionPath = "internal.http_server.handlers.solve.skips_check.New"

		log = log.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", middleware.GetReqID(request.Context())),
		)

		var decodedRequest ClientRequest
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

		// Проверка существования задачи (опционально)
		exists, err := storage.CheckAliasExist(decodedRequest.TaskAlias)
		if err != nil || !exists {
			log.Error("task not found", sl.Err(err))
			writer.WriteHeader(http.StatusNotFound)
			render.JSON(writer, request, getErrorResponse("task not found"))
			return
		}

		// Сохранение посылки
		submissionID, err := storage.SavePendingSubmission(decodedRequest.TaskAlias, decodedRequest.Answers)
		if err != nil {
			log.Error("failed to save submission", sl.Err(err))
			writer.WriteHeader(http.StatusInternalServerError)
			render.JSON(writer, request, getErrorResponse("internal server error"))
			return
		}

		// Возвращаем ID посылки (статус пока Pending)
		response := ServerResponse{
			ResponseInfo: response_info.OK(),
			SubmissionID: submissionID,
		}
		writer.WriteHeader(http.StatusOK)
		render.JSON(writer, request, response)

		// Асинхронная обработка
		go processSubmissionAsync(log, storage, decodedRequest.TaskAlias, submissionID, decodedRequest.Answers)

		log.Info("submission processing initiated", slog.Int64("submission_id", submissionID))
	}
}

// processTaskAsync асинхронно обрабатывает задачу и сохраняет результат
func processSubmissionAsync(log *slog.Logger, storage *database.Storage, taskAlias string, submissionID int64, userAnswers []string) {
	log = log.With(slog.Int64("submission_id", submissionID))

	correctAnswers, err := storage.GetCodeAnswers(taskAlias)
	if err != nil {
		log.Error("Got error while getting correct answers: " + err.Error())
		err := storage.UpdateSubmissionStatusToFailed(submissionID)
		if err != nil {
			log.Error("Error processing submission" + strconv.FormatInt(submissionID, 10) + " while setting Failed status: " + err.Error())
			return
		}
	}

	skipsCode, err := storage.GetSavedCode(taskAlias)
	if err != nil {
		log.Error("Got error while getting saved code: " + err.Error())
		err := storage.UpdateSubmissionStatusToFailed(submissionID)
		if err != nil {
			log.Error("Error processing submission" + strconv.FormatInt(submissionID, 10) + " while setting Failed status: " + err.Error())
			return
		}
	}

	llmResponse, err := processSubmission(correctAnswers, userAnswers, skipsCode, log)
	if err != nil {
		log.Error("Got error while processing submission: " + err.Error())
		err := storage.UpdateSubmissionStatusToFailed(submissionID)
		if err != nil {
			log.Error("Error processing submission" + strconv.FormatInt(submissionID, 10) + " while setting Failed status: " + err.Error())
			return
		}
	}
	if &llmResponse == nil {
		log.Error("Got nil llm response")
		err := storage.UpdateSubmissionStatusToFailed(submissionID)
		if err != nil {
			log.Error("Error processing submission" + strconv.FormatInt(submissionID, 10) + " while setting Failed status: " + err.Error())
			return
		}
	}

	log.Info(fmt.Sprintf("LLM response struct: %+v", *llmResponse))
	log.Info("Processed LLM.", slog.Any("hints", llmResponse.Hints), slog.String("status", llmResponse.Status))

	if llmResponse.Status == "ok" {
		err = storage.UpdateSubmissionStatusToSuccess(submissionID)
		if err != nil {
			log.Error("Got error while setting submission to success: " + err.Error())
			err := storage.UpdateSubmissionStatusToFailed(submissionID)
			if err != nil {
				log.Error("Error processing submission" + strconv.FormatInt(submissionID, 10) + " while setting Failed status: " + err.Error())
				return
			}
		}
		return
	} else {
		hints := make([]string, 0, len(llmResponse.Hints))
		for i := 0; i < len(llmResponse.Hints); i++ {
			hints = append(hints, strconv.Itoa(llmResponse.Hints[i].Index)+"'th skip: "+llmResponse.Hints[i].Message)
		}
		log.Info("Processed LLM.", slog.Any("hints", llmResponse.Hints), slog.String("status", llmResponse.Status))

		err := storage.UpdateSubmissionStatusToFailedWithHints(
			submissionID,
			hints,
		)
		if err != nil {
			log.Error("Got error while setting submission to failed: " + err.Error())
			err := storage.UpdateSubmissionStatusToFailed(submissionID)
			if err != nil {
				log.Error("Error processing submission" + strconv.FormatInt(submissionID, 10) + " while setting Failed status: " + err.Error())
				return
			}
		}
	}
}

type PromptData struct {
	SkipsCode string     `json:"skipsCode"`
	Answers   [][]string `json:"answers"`
}

func encodePrompt(skipsCode string, correctAnswers, userAnswers []string) (string, error) {
	// Проверяем, что длины массивов совпадают
	if len(correctAnswers) != len(userAnswers) {
		return "", fmt.Errorf("length of correctAnswers and userAnswers must be equal")
	}

	// Формируем массив пар [correct_answer, user_answer]
	var answers [][]string
	for i := 0; i < len(correctAnswers); i++ {
		answers = append(answers, []string{correctAnswers[i], userAnswers[i]})
	}

	// Создаём структуру с данными
	prompt := PromptData{
		SkipsCode: skipsCode,
		Answers:   answers,
	}

	// Сериализуем в JSON и преобразуем в строку
	jsonData, err := json.Marshal(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to marshal prompt to JSON: %v", err)
	}

	return string(jsonData), nil
}

func processSubmission(correctAnswers []string, userAnswers []string, skipsCode string, logger *slog.Logger) (*LLMResponse, error) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	model := os.Getenv("MODEL")
	temperature := 0.7

	client := openRouterAPI.NewClient(apiKey, model, temperature)

	prompts, err := openRouterAPI.LoadSystemPrompts("./config/skips_check_prompt.yaml")
	if err != nil {
		log.Fatalf("Error loading system prompts: %v", err)
	}

	systemPrompt, exists := prompts["system_prompt"]
	if !exists {
		log.Fatalf("System prompt not found in the YAML file")
	}

	userPrompt, err := encodePrompt(skipsCode, correctAnswers, userAnswers)
	if err != nil {
		return &LLMResponse{}, err
	}

	response, err := client.SendChat(systemPrompt, userPrompt)
	if err != nil {
		log.Fatalf("Error sending request: %v", err)
	}
	fmt.Println("ServerResponse from OpenRouter:", response)

	var decodedLLMResponse LLMResponse
	cleanedResponse := openRouterAPI.CleanLLMResponse(response)
	fmt.Println("Cleaned response from OpenRouter:", cleanedResponse)
	err = json.Unmarshal([]byte(cleanedResponse), &decodedLLMResponse)
	if err != nil {
		if errors.Is(err, io.EOF) {
			logger.Error("request body is empty")
			return &LLMResponse{}, fmt.Errorf("request body is empty")
		} else {
			logger.Error("failed to decode request body", sl.Err(err))
			return &LLMResponse{}, err
		}
	}

	logger.Info("LLM response body was decoded", slog.Any("decodedLLMResponse", decodedLLMResponse))

	return &decodedLLMResponse, nil
}

func generateAlias(length int) string {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}

	return base64.URLEncoding.EncodeToString(b)[:length]
}
