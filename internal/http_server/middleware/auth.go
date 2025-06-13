package middleware

import (
	"codular-backend/lib/logger/sl"
	"context"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/golang-jwt/jwt/v5"
	"log/slog"
	"net/http"
	"strings"
)

type contextKey string

const (
	UserIDKey contextKey = "user_id"
)

func AuthMiddleware(secret string, log *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			const functionPath = "internal.http_server.middleware.auth.AuthMiddleware"

			log = log.With(
				slog.String("function_path", functionPath),
				slog.String("request_id", r.Context().Value(middleware.RequestIDKey).(string)),
			)

			// Извлечение токена из заголовка Authorization
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				log.Error("authorization header missing")
				w.WriteHeader(http.StatusUnauthorized)
				render.JSON(w, r, map[string]string{"error": "authorization header is required"})
				return
			}

			// Проверка формата Bearer
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				log.Error("invalid authorization header format")
				w.WriteHeader(http.StatusUnauthorized)
				render.JSON(w, r, map[string]string{"error": "invalid authorization header format"})
				return
			}

			// Парсинг и проверка JWT
			token, err := jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrInvalidKey
				}
				return []byte(secret), nil
			})
			if err != nil {
				log.Error("failed to parse token", sl.Err(err))
				w.WriteHeader(http.StatusUnauthorized)
				render.JSON(w, r, map[string]string{"error": "invalid token"})
				return
			}

			if !token.Valid {
				log.Error("token is invalid")
				w.WriteHeader(http.StatusUnauthorized)
				render.JSON(w, r, map[string]string{"error": "invalid token"})
				return
			}

			// Извлечение user_id из claims
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				log.Error("failed to parse claims")
				w.WriteHeader(http.StatusUnauthorized)
				render.JSON(w, r, map[string]string{"error": "invalid token claims"})
				return
			}

			userIDFloat, ok := claims["user_id"].(float64)
			if !ok {
				log.Error("user_id not found in claims")
				w.WriteHeader(http.StatusUnauthorized)
				render.JSON(w, r, map[string]string{"error": "user_id not found in token"})
				return
			}
			userID := int64(userIDFloat)

			// Добавление user_id в контекст
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
