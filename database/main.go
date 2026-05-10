package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

var (
	store        DataStore
	queueManager *QueueManager
)

type APIRequest struct {
	Collection string      `json:"collection"`
	Filter     interface{} `json:"filter"`
	Data       interface{} `json:"data"`
	UserID     string      `json:"user_id"`
}

func main() {
	// 1. Initialize Mongo Store (Agnostic)
	var err error
	store, err = NewMongoStore("mongodb://localhost:27017", "portway")
	if err != nil {
		log.Fatal("❌ Failed to initialize Data Store:", err)
	}
	fmt.Println("🍃 MongoDB Store initialized")

	// 2. Initialize Queue Manager (High-Performance Worker Pool)
	queueManager = NewQueueManager(10, store) // 10 workers
	queueManager.Start()

	// 3. Define Routes
	http.HandleFunc("/data/create", enableCORS(AuthMiddleware(handleCreate)))
	http.HandleFunc("/data/find", enableCORS(AuthMiddleware(handleFind)))
	http.HandleFunc("/data/update", enableCORS(AuthMiddleware(handleUpdate)))
	http.HandleFunc("/data/delete", enableCORS(AuthMiddleware(handleDelete)))
	http.HandleFunc("/health", handleHealth)

	fmt.Println("🚀 Portway Data Proxy running on http://localhost:8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

func handleCreate(w http.ResponseWriter, r *http.Request) {
	var req APIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Enqueue the write operation
	queueManager.Enqueue(DBTask{
		Type:       TaskCreate,
		Collection: req.Collection,
		Data:       req.Data,
		UserID:     req.UserID,
	})

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "queued", "message": "Record creation enqueued"})
}

func handleFind(w http.ResponseWriter, r *http.Request) {
	// Reads are immediate (Synchronous)
	collection := r.URL.Query().Get("collection")
	userID := r.Header.Get("X-User-ID")

	if collection == "" {
		http.Error(w, "Collection name required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Simple filter: only find records for this user (Security)
	filter := map[string]interface{}{"user_id": userID}

	results, err := store.Find(ctx, collection, filter)
	if err != nil {
		http.Error(w, "Search failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func handleUpdate(w http.ResponseWriter, r *http.Request) {
	var req APIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	queueManager.Enqueue(DBTask{
		Type:       TaskUpdate,
		Collection: req.Collection,
		Filter:     req.Filter,
		Data:       req.Data,
		UserID:     req.UserID,
	})

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	var req APIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	queueManager.Enqueue(DBTask{
		Type:       TaskDelete,
		Collection: req.Collection,
		Filter:     req.Filter,
		UserID:     req.UserID,
	})

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "alive"})
}

func enableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-User-ID, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	}
}
