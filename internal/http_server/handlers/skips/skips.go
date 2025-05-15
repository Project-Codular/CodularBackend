package skips

import (
	"codium-backend/internal/config"
	"codium-backend/internal/storage"
	openRouterAPI "codium-backend/lib/api/openrouter"
	response_info "codium-backend/lib/api/response"
	"codium-backend/lib/logger/sl"
	"crypto/rand"
	"encoding/base64"
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
)

type Request struct {
	Code        string `json:"sourceCode" validate:"required"`
	SkipsNumber int    `json:"skipsNumber" validate:"required,gte=0"`
}

type Response struct {
	ResponseInfo    response_info.ResponseInfo `json:"responseInfo"`
	ProcessedCode   string                     `json:"processedCode"`
	ProcessedCodeId string                     `json:"processedCodeId"`
}

//go:generate go run github.com/vektra/mockery/v2@v2.53.3 --name=URLSaver
type SkipsGenerator interface {
	SaveSkipsCode(userCode string, skipsNumber int, alias string) (int64, error)
	GetSkipsCode(codeAlias string) (string, error)
}

func getErrorResponse(msg string) *Response {
	return &Response{
		ResponseInfo:    response_info.Error(msg),
		ProcessedCode:   "",
		ProcessedCodeId: "",
	}
}

func getValidationErrorResponse(validationErrors validator.ValidationErrors) *Response {
	return &Response{
		ResponseInfo:    response_info.ValidationError(validationErrors),
		ProcessedCode:   "",
		ProcessedCodeId: "",
	}
}

func getOKResponse(processedCode string, processedCodeId string) *Response {
	return &Response{
		ResponseInfo:    response_info.OK(),
		ProcessedCode:   processedCode,
		ProcessedCodeId: processedCodeId,
	}
}

// New generates skips for the provided code and saves it to the database.
// @Summary Generate and save skips for code
// @Description Processes the provided source code with a specified number of skips, generates a unique alias, and saves it to the database.
// @Tags Skips
// @Accept json
// @Produce json
// @Param request body Request true "Source code and number of skips"
// @Success 200 {object} Response "Successfully generated and saved skips"
// @Failure 400 {object} Response "Invalid request or empty body"
// @Failure 500 {object} Response "Internal server error"
// @Router /skips/generate [post]
func New(log *slog.Logger, skipsGenerator SkipsGenerator, cfg *config.Config) http.HandlerFunc {
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

		log.Info("request body decoded", slog.Any("decodedRequest", decodedRequest))

		if err := validator.New().Struct(decodedRequest); err != nil {
			var validationErrs validator.ValidationErrors
			errors.As(err, &validationErrs)
			log.Error("invalid request", sl.Err(err))
			writer.WriteHeader(http.StatusBadRequest)
			render.JSON(writer, request, getValidationErrorResponse(validationErrs))
			return
		}

		processedCode, err := processCode(decodedRequest.Code, decodedRequest.SkipsNumber)
		if err != nil {
			log.Error("failed to generate skips for code", sl.Err(err))
			writer.WriteHeader(http.StatusInternalServerError)
			render.JSON(writer, request, getErrorResponse("failed to generate skips for code"))
			return
		}

		isValidAlias := false
		alias := ""
		for !isValidAlias {
			alias = generateAlias(cfg.AliasLength)
			_, err := skipsGenerator.GetSkipsCode(alias)
			if errors.Is(err, storage.ErrCodeNotFound) { // this alias is free
				isValidAlias = true
				break
			}
			if err != nil { // some other error occurred
				log.Error("failed to check alias "+alias+" existence in db", sl.Err(err))
				writer.WriteHeader(http.StatusInternalServerError)
				render.JSON(writer, request, getErrorResponse("failed to check alias existence in db"))
				return
			}
		}

		id, err := skipsGenerator.SaveSkipsCode(decodedRequest.Code, decodedRequest.SkipsNumber, alias)
		if err != nil {
			log.Error("error while saving skips code", sl.Err(err))
			writer.WriteHeader(http.StatusInternalServerError)
			render.JSON(writer, request, getErrorResponse("server error while saving code with skips"))
			return
		}

		log.Info("task saved to db", slog.Int64("_id", id))
		writer.WriteHeader(http.StatusOK)
		render.JSON(writer, request, getOKResponse(processedCode, alias))
	}
}

func processCode(code string, number int) (string, error) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	model := "meta-llama/llama-4-scout:free"
	temperature := 0.7

	client := openRouterAPI.NewClient(apiKey, model, temperature)

	prompts, err := openRouterAPI.LoadSystemPrompts("./config/system_prompts.yaml")
	if err != nil {
		log.Fatalf("Error loading system prompts: %v", err)
	}

	systemPrompt, exists := prompts["omissions"]
	if !exists {
		log.Fatalf("System prompt not found in the YAML file")
	}

	response, err := client.SendChat(systemPrompt, code)
	if err != nil {
		log.Fatalf("Error sending request: %v", err)
	}
	fmt.Println("Response from OpenRouter:", response)

	return response, nil
}

func generateAlias(length int) string {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}

	return base64.URLEncoding.EncodeToString(b)[:length]
}
