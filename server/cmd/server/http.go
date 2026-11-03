package main

import "net/http"

// nosniff applies to all routes, including the mux's own redirects.
func nosniff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Keep the original writer so streaming and WebSocket upgrades work.
		next.ServeHTTP(w, r)
	})
}
