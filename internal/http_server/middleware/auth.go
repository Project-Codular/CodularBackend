package middleware

import (
	response_info "codular-backend/lib/api/response"
	"codular-backend/lib/logger/sl"
	"context"
	"fmt"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/golang-jwt/jwt/v5"
	"log/slog"
	"net/http"
	"strings"
)

type UserIDKeyType string

const UserIDKey UserIDKeyType = "user_id"

// AuthMiddleware проверяет JWT access-токен
func AuthMiddleware(jwtSecret string, log *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			const functionPath = "internal.http_server.middleware.AuthMiddleware"

			log = log.With(
				slog.String("function_path", functionPath),
				slog.String("request_id", r.Context().Value(chiMiddleware.RequestIDKey).(string)),
			)

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				log.Error("missing Authorization header")
				w.WriteHeader(http.StatusUnauthorized)
				render.JSON(w, r, response_info.Error("missing Authorization header"))
				return
			}

			if !strings.HasPrefix(authHeader, "Bearer ") {
				log.Error("invalid Authorization header format")
				w.WriteHeader(http.StatusUnauthorized)
				render.JSON(w, r, response_info.Error("invalid Authorization header format"))
				return
			}

			tokenString := strings.TrimPrefix(authHeader, "Bearer ")

			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}
				return []byte(jwtSecret), nil
			})
			if err != nil {
				log.Error("failed to parse token", sl.Err(err))
				w.WriteHeader(http.StatusUnauthorized)
				render.JSON(w, r, response_info.Error("invalid or expired token"))
				return
			}

			if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
				userID, ok := claims["user_id"].(float64)
				if !ok {
					log.Error("user_id not found in token")
					w.WriteHeader(http.StatusUnauthorized)
					render.JSON(w, r, response_info.Error("invalid token"))
					return
				}

				// Добавление user_id в контекст
				ctx := context.WithValue(r.Context(), UserIDKey, int64(userID))
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				log.Error("invalid token claims")
				w.WriteHeader(http.StatusUnauthorized)
				render.JSON(w, r, response_info.Error("invalid token"))
			}
		})
	}
}
