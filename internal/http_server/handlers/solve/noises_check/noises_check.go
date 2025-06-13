package noises_check

import (
	"codular-backend/internal/storage/database"
	openRouterAPI "codular-backend/lib/api/openrouter"
	response_info "codular-backend/lib/api/response"
	"codular-backend/lib/logger/sl"
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
	TaskAlias string `json:"taskAlias" validate:"required"`
	Answer    string `json:"answer" validate:"required"`
}

type ServerResponse struct {
	ResponseInfo response_info.ResponseInfo `json:"responseInfo"`
	SubmissionID int64                      `json:"submissionId"`
}

type LLMResponse struct {
	Score int      `json:"score"`
	Hints []string `json:"hints"`
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

// New handles the submission of answers for a noises task.
// @Summary Submit answers for a noises task
// @Description Receives user answers for a given task alias, saves the submission, and asynchronously processes it.
// @Tags Noises
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

		// Проверка существования задачи
		exists, err := storage.CheckAliasExist(decodedRequest.TaskAlias)
		if err != nil {
			log.Error("error while finding task with id", sl.Err(err))
			writer.WriteHeader(http.StatusInternalServerError)
			render.JSON(writer, request, getErrorResponse("error while finding task with id"))
			return
		}
		if !exists {
			log.Error("task not found", sl.Err(err))
			writer.WriteHeader(http.StatusNotFound)
			render.JSON(writer, request, getErrorResponse("task not found"))
			return
		}

		// Сохранение посылки
		submissionID, err := storage.SavePendingSubmission(decodedRequest.TaskAlias, []string{decodedRequest.Answer})
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
		go processSubmissionAsync(log, storage, decodedRequest.TaskAlias, submissionID, decodedRequest.Answer)

		log.Info("submission processing initiated", slog.Int64("submission_id", submissionID))
	}
}

// processTaskAsync асинхронно обрабатывает задачу и сохраняет результат
func processSubmissionAsync(log *slog.Logger, storage *database.Storage, taskAlias string, submissionID int64, userAnswer string) {
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

	taskCode, err := storage.GetSavedTaskCode(taskAlias)
	if err != nil {
		log.Error("Got error while getting saved code: " + err.Error())
		err := storage.UpdateSubmissionStatusToFailed(submissionID)
		if err != nil {
			log.Error("Error processing submission" + strconv.FormatInt(submissionID, 10) + " while setting Failed status: " + err.Error())
			return
		}
	}

	llmResponse, err := processSubmission(correctAnswers[0], taskCode, userAnswer, log)
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
	log.Info("Processed LLM.", slog.Any("hints", llmResponse.Hints), slog.Int("score", llmResponse.Score))

	if llmResponse.Score >= 100 {
		err = storage.UpdateSubmissionStatusToSuccess(submissionID, llmResponse.Score)
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
			hints = append(hints, llmResponse.Hints[i])
		}
		log.Info("Processed LLM.", slog.Any("hints", llmResponse.Hints), slog.Int("score", llmResponse.Score))

		err := storage.UpdateSubmissionStatusToFailedWithHints(
			submissionID,
			hints,
			llmResponse.Score,
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

func processSubmission(originalCode string, noisedCode string, userSolutionCode string, logger *slog.Logger) (*LLMResponse, error) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	model := os.Getenv("MODEL")
	temperature := 0.7

	client := openRouterAPI.NewClient(apiKey, model, temperature)

	prompts, err := openRouterAPI.LoadSystemPrompts("./config/noises_check_prompt.yaml")
	if err != nil {
		log.Fatalf("Error loading system prompts: %v", err)
	}

	systemPrompt, exists := prompts["system_prompt"]
	if !exists {
		log.Fatalf("System prompt not found in the YAML file")
	}

	response, err := client.SendChat(systemPrompt, "Исходный код:\n"+originalCode+"\nЗашумленный код:\n"+noisedCode+"\nРешение пользователя:\n"+userSolutionCode)
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
