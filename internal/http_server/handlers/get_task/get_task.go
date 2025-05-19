package get_task

import (
	"codium-backend/internal/storage/postgresql"
	response_info "codium-backend/lib/api/response"
	"codium-backend/lib/logger/sl"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"net/http"
)

type Response struct {
	ResponseInfo response_info.ResponseInfo `json:"responseInfo"`
	CodeToSolve  string                     `json:"codeToSolve"`
}

func getErrorResponse(msg string) *Response {
	return &Response{
		ResponseInfo: response_info.Error(msg),
		CodeToSolve:  "",
	}
}

func getOKResponse(codeToSolve string) *Response {
	return &Response{
		ResponseInfo: response_info.OK(),
		CodeToSolve:  codeToSolve,
	}
}

func New(logger *slog.Logger, storage *postgresql.Storage) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		const functionPath = "internal.http_server.handlers.get_task.New"

		log := logger.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", middleware.GetReqID(request.Context())),
		)

		alias := chi.URLParam(request, "alias")
		if alias == "" {
			log.Error("task alias is empty")
			writer.WriteHeader(http.StatusBadRequest)
			render.JSON(writer, request, getErrorResponse("task alias is empty"))
			return
		}

		log.Info("got alias from request path", slog.String("alias", alias))

		codeFromDb, err := storage.GetSavedCode(alias)
		if err != nil {
			log.Error("failed to get code", sl.Err(err))
			writer.WriteHeader(http.StatusInternalServerError)
			render.JSON(writer, request, getErrorResponse("failed to get code with specified alias"))
			return
		}

		log.Info("got task by alias from db", slog.String("alias", alias))
		writer.WriteHeader(http.StatusOK)
		render.JSON(writer, request, getOKResponse(codeFromDb))
	}
}
