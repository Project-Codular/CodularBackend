package submission_status

import (
	"codular-backend/internal/storage/database"
	response_info "codular-backend/lib/api/response"
	"codular-backend/lib/logger/sl"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"net/http"
	"strconv"
)

type ClientRequest struct {
	SubmissionId int64 `json:"submissionId" validate:"required"`
}

type ServerResponse struct {
	ResponseInfo response_info.ResponseInfo `json:"responseInfo"`
	Status       string                     `json:"isCorrect"`
	Score        int                        `json:"score"`
	Hints        []string                   `json:"hints"`
}

func getErrorResponse(msg string) *ServerResponse {
	return &ServerResponse{
		ResponseInfo: response_info.Error(msg),
		Status:       "",
		Hints:        make([]string, 0),
	}
}

// GetSubmissionStatus returns the status of a submission by its ID.
// @Summary Get submission status
// @Description Returns the current status of a submission by its ID, including any hints if available.
// @Tags Submissions
// @Produce json
// @Param submission_id path int true "Submission ID"
// @Success 200 {object} ServerResponse "Submission status retrieved successfully"
// @Success 200 {object} ServerResponse "Example response for success" Example({"responseInfo":{"status":"OK"},"IsCorrect":"Success","hints":[]})
// @Success 200 {object} ServerResponse "Example response for failure with hints" Example({"responseInfo":{"status":"OK"},"IsCorrect":"Failed","hints":["1'th skip: Hint message 1","3'th skip: Hint message 2"]})
// @Failure 400 {object} ServerResponse "Invalid submission ID format"
// @Failure 404 {object} ServerResponse "Submission not found"
// @Failure 500 {object} ServerResponse "Internal server error"
// @Router /submission-status/{submission_id} [get]
func New(log *slog.Logger, storage *database.Storage) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		const functionPath = "internal.http_server.handlers.submission_status.New"

		log = log.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", middleware.GetReqID(request.Context())),
		)

		// Получение submission_id из пути
		submissionIDStr := chi.URLParam(request, "submission_id")
		if submissionIDStr == "" {
			log.Error("submission_id parameter is missing")
			writer.WriteHeader(http.StatusBadRequest)
			render.JSON(writer, request, getErrorResponse("submission_id parameter is missing"))
			return
		}

		// Преобразование submission_id в int64
		submissionID, err := strconv.ParseInt(submissionIDStr, 10, 64)
		if err != nil {
			log.Error("invalid submission_id format", sl.Err(err))
			writer.WriteHeader(http.StatusBadRequest)
			render.JSON(writer, request, getErrorResponse("invalid submission_id format"))
			return
		}

		// Формирование ответа
		submissionStatus, err := storage.GetFullSubmissionStatus(submissionID)
		if errors.Is(err, fmt.Errorf("submission not found")) {
			log.Error("submission not found", slog.Int64("submission_id", submissionID))
			writer.WriteHeader(http.StatusNotFound)
			render.JSON(writer, request, getErrorResponse("submission not found"))
			return
		}
		if err != nil {
			log.Error("failed to get submission status", sl.Err(err))
			writer.WriteHeader(http.StatusInternalServerError)
			render.JSON(writer, request, getErrorResponse("internal server error"))
			return
		}

		log.Info("Got submission hints", slog.Any("hints", submissionStatus.Hints))
		log.Info("Got submission score", slog.Int("score", submissionStatus.Score))

		writer.WriteHeader(http.StatusOK)
		render.JSON(writer, request, ServerResponse{
			ResponseInfo: response_info.OK(),
			Status:       submissionStatus.Status,
			Score:        submissionStatus.Score,
			Hints:        submissionStatus.Hints,
		})

		log.Info("submission status retrieved", slog.Int64("submission_id", submissionID), slog.String("status", submissionStatus.Status))
	}
}
