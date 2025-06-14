package get_task

import (
	"codular-backend/internal/storage/database"
	"codular-backend/lib/logger/sl"
	"github.com/go-chi/chi/v5/middleware"
	"log/slog"
	"net/http"
)

// RandomTask redirects to a random public task.
// @Summary Get random public task
// @Description Redirects to a random public task with public = true. The redirected endpoint returns the task code and description.
// @Tags Task
// @Produce json
// @Success 302 {string} string "Redirect to /api/v1/task/{alias}"
// @Failure 404 {object} map[string]string "No public tasks found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /task/random [get]
func RandomTask(log *slog.Logger, storage *database.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const functionPath = "internal.http_server.handlers.task.RandomTask"

		log = log.With(
			slog.String("function_path", functionPath),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		alias, err := storage.GetRandomPublicTaskAlias()
		if err != nil {
			if err.Error() == "no public tasks found" {
				log.Warn("no public tasks found")
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"error":"no public tasks found"}`))
				return
			}
			log.Error("failed to get random public task", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"internal server error"}`))
			return
		}

		redirectURL := "/api/v1/task/" + alias
		log.Info("redirecting to random public task", slog.String("alias", alias))
		http.Redirect(w, r, redirectURL, http.StatusFound)
	}
}
