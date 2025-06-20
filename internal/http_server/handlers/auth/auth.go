package auth

import (
	"codular-backend/internal/storage/database"
	response_info "codular-backend/lib/api/response"
	"codular-backend/lib/logger/sl"
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

type AuthResponse struct {
	ResponseInfo response_info.ResponseInfo `json:"responseInfo"`
	AccessToken  string                     `json:"access_token"`
}

type UserStorage interface {
	CreateUser(email, passwordHash string) (int64, error)
	GetUserByEmail(email string) (int64, string, error)
	SaveToken(userID int64, token, tokenType string, expiresAt time.Time) error
	ValidateToken(token, tokenType string) (int64, bool, error)
	DeleteToken(token, tokenType string) error
}

// getErrorResponse возвращает ответ с ошибкой
func getErrorResponse(msg string) *AuthResponse {
	return &AuthResponse{
		ResponseInfo: response_info.Error(msg),
		AccessToken:  "",
	}
}

// getValidationErrorResponse возвращает ответ с ошибками валидации
func getValidationErrorResponse(validationErrors validator.ValidationErrors) *AuthResponse {
	return &AuthResponse{
		ResponseInfo: response_info.ValidationError(validationErrors),
		AccessToken:  "",
	}
}

// getOKResponse возвращает успешный ответ с токеном
func getOKResponse(accessToken string) *AuthResponse {
	return &AuthResponse{
		ResponseInfo: response_info.OK(),
		AccessToken:  accessToken,
	}
}

// setRefreshTokenCookie устанавливает refresh token в secure, HTTP-only cookie
func setRefreshTokenCookie(w http.ResponseWriter, refreshToken string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Expires:  expiresAt,
		Path:     "/api/v1/auth",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// clearRefreshTokenCookie удаляет refresh token cookie
func clearRefreshTokenCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Expires:  time.Now().Add(-time.Hour),
		Path:     "/api/v1/auth",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// Register создаёт нового пользователя
// @Summary Register a new user
// @Description Registers a new user with email and password, returning access token and setting refresh token in a secure cookie.
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "User email and password"
// @Success 200 {object} AuthResponse "User registered successfully"
// @Failure 400 {object} AuthResponse "Invalid request or empty body"
// @Failure 409 {object} AuthResponse "Email already exists"
// @Failure 500 {object} AuthResponse "Internal server error"
// @Router /auth/register [post]
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

		if err := storage.SaveToken(userID, accessToken, "access", time.Now().Add(time.Hour*24)); err != nil {
			log.Error("failed to save access token", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		if err := storage.SaveToken(userID, refreshToken, "refresh", time.Now().Add(time.Hour*24*30)); err != nil {
			log.Error("failed to save refresh token", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		setRefreshTokenCookie(w, refreshToken, time.Now().Add(time.Hour*24*30))
		w.WriteHeader(http.StatusOK)
		render.JSON(w, r, getOKResponse(accessToken))
		log.Info("user registered", slog.Int64("user_id", userID))
	}
}

// Login аутентифицирует пользователя
// @Summary Login user
// @Description Authenticates a user with email and password, returning access token and setting refresh token in a secure cookie.
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "User email and password"
// @Success 200 {object} AuthResponse "User logged in successfully"
// @Failure 400 {object} AuthResponse "Invalid request or empty body"
// @Failure 401 {object} AuthResponse "Invalid credentials"
// @Failure 500 {object} AuthResponse "Internal server error"
// @Router /auth/login [post]
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

		if err := storage.SaveToken(userID, accessToken, "access", time.Now().Add(time.Hour*24)); err != nil {
			log.Error("failed to save access token", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		if err := storage.SaveToken(userID, refreshToken, "refresh", time.Now().Add(time.Hour*24*30)); err != nil {
			log.Error("failed to save refresh token", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		setRefreshTokenCookie(w, refreshToken, time.Now().Add(time.Hour*24*30))
		w.WriteHeader(http.StatusOK)
		render.JSON(w, r, getOKResponse(accessToken))
		log.Info("user logged in", slog.Int64("user_id", userID))
	}
}

// Refresh обновляет access-токен по refresh-токену из cookie
// @Summary Refresh access token
// @Description Refreshes the access token using a valid refresh token provided in a secure cookie.
// @Tags Auth
// @Produce json
// @Success 200 {object} AuthResponse "Access token refreshed successfully"
// @Failure 400 {object} AuthResponse "Missing refresh token cookie"
// @Failure 401 {object} AuthResponse "Invalid or expired refresh token"
// @Failure 500 {object} AuthResponse "Internal server error"
// @Router /auth/refresh [post]
func Refresh(log *slog.Logger, storage *database.Storage, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const functionPath = "internal.http_server.handlers.auth.Refresh"

		log = log.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		refreshCookie, err := r.Cookie("refresh_token")
		if err != nil {
			log.Error("missing refresh token cookie", sl.Err(err))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getErrorResponse("missing refresh token cookie"))
			return
		}

		userID, valid, err := storage.ValidateToken(refreshCookie.Value, "refresh")
		if err != nil {
			log.Error("failed to validate refresh token", sl.Err(err))
			w.WriteHeader(http.StatusUnauthorized)
			render.JSON(w, r, getErrorResponse("invalid or expired refresh token"))
			return
		}
		if !valid {
			log.Error("invalid refresh token")
			w.WriteHeader(http.StatusUnauthorized)
			render.JSON(w, r, getErrorResponse("invalid or expired refresh token"))
			return
		}

		// Генерация нового access-токена
		newAccessToken, err := generateJWT(userID, jwtSecret, time.Hour*24)
		if err != nil {
			log.Error("failed to generate access JWT", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		// Генерация нового refresh-токена
		newRefreshToken, err := generateJWT(userID, jwtSecret, time.Hour*24*30)
		if err != nil {
			log.Error("failed to generate refresh JWT", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		// Сохранение новых токенов
		if err := storage.SaveToken(userID, newAccessToken, "access", time.Now().Add(time.Hour*24)); err != nil {
			log.Error("failed to save access token", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		if err := storage.SaveToken(userID, newRefreshToken, "refresh", time.Now().Add(time.Hour*24*30)); err != nil {
			log.Error("failed to save refresh token", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		// Удаление старого refresh-токена
		if err := storage.DeleteToken(refreshCookie.Value, "refresh"); err != nil {
			log.Error("failed to delete old refresh token", sl.Err(err))
			// Не возвращаем ошибку клиенту, так как новые токены уже выданы
		}

		setRefreshTokenCookie(w, newRefreshToken, time.Now().Add(time.Hour*24*30))
		w.WriteHeader(http.StatusOK)
		render.JSON(w, r, getOKResponse(newAccessToken))
		log.Info("access token refreshed", slog.Int64("user_id", userID))
	}
}

// Logout завершает сессию пользователя
// @Summary Logout user
// @Description Invalidates the refresh token by clearing the secure cookie and removing it from the database.
// @Tags Auth
// @Produce json
// @Success 200 {object} AuthResponse "User logged out successfully"
// @Failure 400 {object} AuthResponse "Missing refresh token cookie"
// @Failure 500 {object} AuthResponse "Internal server error"
// @Router /auth/logout [post]
func Logout(log *slog.Logger, storage *database.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const functionPath = "internal.http_server.handlers.auth.Logout"

		log = log.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		refreshCookie, err := r.Cookie("refresh_token")
		if err != nil {
			log.Error("missing refresh token cookie", sl.Err(err))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, getErrorResponse("missing refresh token cookie"))
			return
		}

		// Удаление refresh-токена из базы
		if err := storage.DeleteToken(refreshCookie.Value, "refresh"); err != nil {
			log.Error("failed to delete refresh token", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, getErrorResponse("internal server error"))
			return
		}

		// Очистка cookie
		clearRefreshTokenCookie(w)
		w.WriteHeader(http.StatusOK)
		render.JSON(w, r, getOKResponse(""))
		log.Info("user logged out")
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
