package main

import (
	"net/http"
)

// AuthMiddleware now simply ensures a user_id is provided.
// It assumes the user has already been authenticated by your main Node.js backend.
func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get user_id from query or header
		userID := r.URL.Query().Get("user_id")
		if userID == "" {
			userID = r.Header.Get("X-User-ID")
		}

		if userID == "" {
			http.Error(w, "user_id is required", http.StatusUnauthorized)
			return
		}

		// Pass the request to the next handler
		next.ServeHTTP(w, r)
	}
}
