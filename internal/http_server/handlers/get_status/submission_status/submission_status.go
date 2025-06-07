package submission_status

import (
	"codium-backend/internal/storage/database"
	response_info "codium-backend/lib/api/response"
	"codium-backend/lib/logger/sl"
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
	Status       string                     `json:"IsCorrect"`
	Hints        []string                   `json:"hints"`
}

func getErrorResponse(msg string) *ServerResponse {
	return &ServerResponse{
		ResponseInfo: response_info.Error(msg),
		Status:       "",
		Hints:        make([]string, 0),
	}
}

// GetSubmissionStatus возвращает статус посылки по её ID
// @Summary Get submission status
// @Description Returns the current status of a submission by its ID, including any hints if available.
// @Tags Submissions
// @Produce json
// @Param submission_id path int true "Submission ID"
// @Success 200 {object} SubmissionStatusResponse "Submission status retrieved successfully"
// @Success 200 {object} SubmissionStatusResponse "Example response" Example({"status":"Success","hints":[]})
// @Failure 400 {object} SubmissionStatusResponse "Invalid submission ID"
// @Failure 404 {object} SubmissionStatusResponse "Submission not found"
// @Failure 500 {object} SubmissionStatusResponse "Internal server error"
// @Router /submission-status/{submission_id} [get]
func GetSubmissionStatus(log *slog.Logger, storage *database.Storage) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		const functionPath = "internal.http_server.handlers.submission_status.GetSubmissionStatus"

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
		submissionStatus, err := storage.GetSubmissionStatus(submissionID)
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

		submissionHits, err := storage.GetSubmissionHints(submissionID)
		if err != nil {
			log.Error("failed to get submission hints", sl.Err(err))
			writer.WriteHeader(http.StatusInternalServerError)
			render.JSON(writer, request, getErrorResponse("internal server error"))
			return
		}

		log.Info("Got submission hints", slog.Any("hints", submissionHits))

		writer.WriteHeader(http.StatusOK)
		render.JSON(writer, request, ServerResponse{
			ResponseInfo: response_info.OK(),
			Status:       submissionStatus,
			Hints:        submissionHits,
		})

		log.Info("submission status retrieved", slog.Int64("submission_id", submissionID), slog.String("status", submissionStatus))
	}
}
