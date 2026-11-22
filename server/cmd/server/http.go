package main

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// nosniff applies to all routes, including the mux's own redirects.
func nosniff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Keep the original writer so streaming and WebSocket upgrades work.
		next.ServeHTTP(w, r)
	})
}

// health is /healthz, for probes: 200 while the database answers. It
// says nothing else about the server.
func health(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		var one int
		if err := db.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
			slog.Error("health check", "err", err)
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, "ok\n")
	}
}
