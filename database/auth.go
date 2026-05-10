package main

import (
	"fmt"
	"net/http"
)

// AuthMiddleware ensures the request is authorized.
// In a real scenario, this would call your Node.js Auth Backend 
// or verify a JWT token.
func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get user_id or token from headers
		userID := r.Header.Get("X-User-ID")
		token := r.Header.Get("Authorization")

		if userID == "" && token == "" {
			fmt.Println("🚫 Unauthorized access attempt blocked")
			http.Error(w, "Unauthorized: user_id or token required", http.StatusUnauthorized)
			return
		}

		// Optional: Call Node.js Auth API (http://localhost:5000/api/auth/verify)
		// For now, we trust the header if present, but we'll log it.
		// fmt.Printf("👤 Request authenticated for User: %s\n", userID)

		next.ServeHTTP(w, r)
	}
}
