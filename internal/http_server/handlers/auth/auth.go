package auth

import (
	response_info "codular-backend/lib/api/response"
	"codular-backend/lib/logger/sl"
	"fmt"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/go-playground/validator/v10"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"log/slog"
	"net/http"
	"time"
)

type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

type AuthResponse struct {
	ResponseInfo response_info.ResponseInfo `json:"responseInfo"`
	AccessToken  string                     `json:"access_token"`
	RefreshToken string                     `json:"refresh_token,omitempty"`
}

type UserStorage interface {
	CreateUser(email, passwordHash string) (int64, error)
	GetUserByEmail(email string) (int64, string, error)
	SaveRefreshToken(userID int64, refreshToken string) error
	ValidateRefreshToken(userID int64, refreshToken string) (bool, error)
}

// getErrorResponse возвращает ответ с ошибкой
func getErrorResponse(msg string) *AuthResponse {
	return &AuthResponse{
		ResponseInfo: response_info.Error(msg),
		AccessToken:  "",
		RefreshToken: "",
	}
}

// getValidationErrorResponse возвращает ответ с ошибками валидации
func getValidationErrorResponse(validationErrors validator.ValidationErrors) *AuthResponse {
	return &AuthResponse{
		ResponseInfo: response_info.ValidationError(validationErrors),
		AccessToken:  "",
		RefreshToken: "",
	}
}

// getOKResponse возвращает успешный ответ с токенами
func getOKResponse(accessToken, refreshToken string) *AuthResponse {
	return &AuthResponse{
		ResponseInfo: response_info.OK(),
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}
}

// Register создаёт нового пользователя
func Register(log *slog.Logger, storage UserStorage, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const functionPath = "internal.http_server.handlers.auth.Register"

		log = log.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		var req RegisterRequest
		if err := render.DecodeJSON(r.Body, &req); err != nil {
			log.Error("failed to decode request body", sl.Err(err))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getErrorResponse("invalid request body"))
			return
		}

		if err := validator.New().Struct(req); err != nil {
			log.Error("invalid request", sl.Err(err))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getValidationErrorResponse(err.(validator.ValidationErrors)))
			return
		}

		passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			log.Error("failed to hash password", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		userID, err := storage.CreateUser(req.Email, string(passwordHash))
		if err != nil {
			if err.Error() == "email already exists" {
				log.Error("email already exists", sl.Err(err))
				w.WriteHeader(http.StatusConflict)
				render.JSON(w, r, getErrorResponse("email already exists"))
				return
			}
			log.Error("failed to create user", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		accessToken, err := generateJWT(userID, jwtSecret, time.Hour*24)
		if err != nil {
			log.Error("failed to generate access JWT", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		refreshToken, err := generateJWT(userID, jwtSecret, time.Hour*24*30)
		if err != nil {
			log.Error("failed to generate refresh JWT", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		if err := storage.SaveRefreshToken(userID, refreshToken); err != nil {
			log.Error("failed to save refresh token", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		w.WriteHeader(http.StatusOK)
		render.JSON(w, r, getOKResponse(accessToken, refreshToken))
		log.Info("user registered", slog.Int64("user_id", userID))
	}
}

// Login аутентифицирует пользователя
func Login(log *slog.Logger, storage UserStorage, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const functionPath = "internal.http_server.handlers.auth.Login"

		log = log.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		var req LoginRequest
		if err := render.DecodeJSON(r.Body, &req); err != nil {
			log.Error("failed to decode request body", sl.Err(err))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getErrorResponse("invalid request body"))
			return
		}

		if err := validator.New().Struct(req); err != nil {
			log.Error("invalid request", sl.Err(err))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getValidationErrorResponse(err.(validator.ValidationErrors)))
			return
		}

		userID, passwordHash, err := storage.GetUserByEmail(req.Email)
		if err != nil {
			log.Error("failed to get user", sl.Err(err))
			w.WriteHeader(http.StatusUnauthorized)
			render.JSON(w, r, getErrorResponse("invalid credentials"))
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
			log.Error("invalid password")
			w.WriteHeader(http.StatusUnauthorized)
			render.JSON(w, r, getErrorResponse("invalid credentials"))
			return
		}

		accessToken, err := generateJWT(userID, jwtSecret, time.Hour*24)
		if err != nil {
			log.Error("failed to generate access JWT", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		refreshToken, err := generateJWT(userID, jwtSecret, time.Hour*24*30)
		if err != nil {
			log.Error("failed to generate refresh JWT", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		if err := storage.SaveRefreshToken(userID, refreshToken); err != nil {
			log.Error("failed to save refresh token", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		w.WriteHeader(http.StatusOK)
		render.JSON(w, r, getOKResponse(accessToken, refreshToken))
		log.Info("user logged in", slog.Int64("user_id", userID))
	}
}

// Refresh обновляет access-токен по refresh-токену
// @Summary Refresh access token
// @Description Refreshes the access token using a valid refresh token.
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RefreshRequest true "Refresh token"
// @Success 200 {object} AuthResponse "Access token refreshed successfully"
// @Failure 400 {object} AuthResponse "Invalid request or empty body"
// @Failure 401 {object} AuthResponse "Invalid refresh token"
// @Failure 500 {object} AuthResponse "Internal server error"
// @Router /auth/refresh [post]
func Refresh(log *slog.Logger, storage UserStorage, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const functionPath = "internal.http_server.handlers.auth.Refresh"

		log = log.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		var req RefreshRequest
		if err := render.DecodeJSON(r.Body, &req); err != nil {
			log.Error("failed to decode request body", sl.Err(err))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getErrorResponse("invalid request body"))
			return
		}

		if err := validator.New().Struct(req); err != nil {
			log.Error("invalid request", sl.Err(err))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getValidationErrorResponse(err.(validator.ValidationErrors)))
			return
		}

		token, err := jwt.Parse(req.RefreshToken, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(jwtSecret), nil
		})
		if err != nil {
			log.Error("failed to parse refresh token", sl.Err(err))
			w.WriteHeader(http.StatusUnauthorized)
			render.JSON(w, r, getErrorResponse("invalid refresh token"))
			return
		}

		if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
			userID, ok := claims["user_id"].(float64)
			if !ok {
				log.Error("user_id not found in token")
				w.WriteHeader(http.StatusUnauthorized)
				render.JSON(w, r, getErrorResponse("invalid refresh token"))
				return
			}

			valid, err := storage.ValidateRefreshToken(int64(userID), req.RefreshToken)
			if err != nil {
				log.Error("failed to validate refresh token", sl.Err(err))
				w.WriteHeader(http.StatusInternalServerError)
				render.JSON(w, r, getErrorResponse("internal server error"))
				return
			}
			if !valid {
				log.Error("invalid refresh token")
				w.WriteHeader(http.StatusUnauthorized)
				render.JSON(w, r, getErrorResponse("invalid refresh token"))
				return
			}

			accessToken, err := generateJWT(int64(userID), jwtSecret, time.Hour*24)
			if err != nil {
				log.Error("failed to generate access JWT", sl.Err(err))
				w.WriteHeader(http.StatusInternalServerError)
				render.JSON(w, r, getErrorResponse("internal server error"))
				return
			}

			// Опционально: обновить refresh-токен
			newRefreshToken, err := generateJWT(int64(userID), jwtSecret, time.Hour*24*30)
			if err != nil {
				log.Error("failed to generate refresh JWT", sl.Err(err))
				w.WriteHeader(http.StatusInternalServerError)
				render.JSON(w, r, getErrorResponse("internal server error"))
				return
			}

			if err := storage.SaveRefreshToken(int64(userID), newRefreshToken); err != nil {
				log.Error("failed to save refresh token", sl.Err(err))
				w.WriteHeader(http.StatusInternalServerError)
				render.JSON(w, r, getErrorResponse("internal server error"))
				return
			}

			w.WriteHeader(http.StatusOK)
			render.JSON(w, r, getOKResponse(accessToken, newRefreshToken))
			log.Info("access token refreshed", slog.Int64("user_id", int64(userID)))
			return
		}

		log.Error("invalid refresh token")
		w.WriteHeader(http.StatusUnauthorized)
		render.JSON(w, r, getErrorResponse("invalid refresh token"))
	}
}

// generateJWT создаёт JWT-токен для пользователя
func generateJWT(userID int64, secret string, duration time.Duration) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(duration).Unix(),
	})
	return token.SignedString([]byte(secret))
}
