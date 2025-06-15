package get_user_email

import (
	my_middleware "codular-backend/internal/http_server/middleware"
	"codular-backend/internal/storage/database"
	response_info "codular-backend/lib/api/response"
	"codular-backend/lib/logger/sl"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"net/http"
)

type Response struct {
	ResponseInfo response_info.ResponseInfo `json:"responseInfo"`
	Email        string                     `json:"email"`
}

func getErrorResponse(msg string) *Response {
	return &Response{
		ResponseInfo: response_info.Error(msg),
		Email:        "",
	}
}

func getOKResponse(email string) *Response {
	return &Response{
		ResponseInfo: response_info.OK(),
		Email:        email,
	}
}

// GetUserEmail retrieves the email associated with the authenticated user's access token.
// @Summary Get user email
// @Description Retrieves the email of the authenticated user based on the access token provided in the Authorization header.
// @Tags User
// @Produce json
// @Success 200 {object} get_user_email.Response "Successfully retrieved user email"
// @Success 200 {object} get_user_email.Response "Example response" Example({"responseInfo":{"status":"OK"},"email":"user@example.com"})
// @Failure 401 {object} get_user_email.Response "Unauthorized"
// @Failure 404 {object} get_user_email.Response "User not found"
// @Failure 500 {object} get_user_email.Response "Internal server error"
// @Security Bearer
// @Router /user/email [get]
func GetUserEmail(logger *slog.Logger, storage *database.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const functionPath = "internal.http_server.handlers.user.GetUserEmail"

		log := logger.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		// Извлечение user_id из контекста
		userID, ok := r.Context().Value(my_middleware.UserIDKey).(int64)
		if !ok {
			log.Error("failed to get user_id from context")
			w.WriteHeader(http.StatusUnauthorized)
			render.JSON(w, r, getErrorResponse("unauthorized"))
			return
		}

		log.Info("retrieving email for user", slog.Int64("user_id", userID))

		// Получение email пользователя
		email, err := storage.GetUserEmailByID(userID)
		if err != nil {
			if err.Error() == "user not found" {
				log.Warn("user not found", slog.Int64("user_id", userID))
				w.WriteHeader(http.StatusNotFound)
				render.JSON(w, r, getErrorResponse("user not found"))
				return
			}
			log.Error("failed to get user email", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		log.Info("successfully retrieved user email", slog.String("email", email))
		w.WriteHeader(http.StatusOK)
		render.JSON(w, r, getOKResponse(email))
	}
}
