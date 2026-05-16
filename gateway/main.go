package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// Define our microservice endpoints
const (
	AuthServiceURL       = "http://localhost:5001"
	DeploymentServiceURL = "http://localhost:8080"
	DatabaseServiceURL   = "http://localhost:8081"
)

func main() {
	// 1. Create proxies
	authProxy := newProxy(AuthServiceURL)
	deployProxy := newProxy(DeploymentServiceURL)
	databaseProxy := newProxy(DatabaseServiceURL)

	// 2. Route definitions
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers for ALL requests
		setCORSHeaders(w)

		// Handle pre-flight OPTIONS requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		path := r.URL.Path
		fmt.Printf("🌐 [GATEWAY] %s %s\n", r.Method, path)

		// Routing Logic
		switch {
		case strings.HasPrefix(path, "/api/auth"):
			// Forward directly to Auth Service (it expects the /api/auth prefix)
			authProxy.ServeHTTP(w, r)

		case strings.HasPrefix(path, "/connect"), strings.HasPrefix(path, "/vps"), strings.HasPrefix(path, "/deploy"), strings.HasPrefix(path, "/exec"), strings.HasPrefix(path, "/terminal"), strings.HasPrefix(path, "/metrics"), strings.HasPrefix(path, "/files"), strings.HasPrefix(path, "/git"), strings.HasPrefix(path, "/search"), strings.HasPrefix(path, "/health"), strings.HasPrefix(path, "/status"):
			// These go to the deployment orchestrator
			deployProxy.ServeHTTP(w, r)

		case strings.HasPrefix(path, "/data"):
			// Data proxy directly
			databaseProxy.ServeHTTP(w, r)

		default:
			http.NotFound(w, r)
		}
	})

	port := ":8888"
	fmt.Println("🚀 Portway API Gateway running on http://localhost" + port)
	fmt.Println("   - Auth: " + AuthServiceURL)
	fmt.Println("   - Deploy: " + DeploymentServiceURL)
	fmt.Println("   - Database: " + DatabaseServiceURL)
	
	log.Fatal(http.ListenAndServe(port, nil))
}

func newProxy(target string) *httputil.ReverseProxy {
	url, _ := url.Parse(target)
	proxy := httputil.NewSingleHostReverseProxy(url)
	
	// Optional: You can modify the request/response here
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = url.Host
	}
	
	return proxy
}

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-User-ID, Authorization")
}
