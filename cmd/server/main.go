// Package main is the entry point for starcat-sharing-api.
// It serves a share-link API: POST creates, GET renders, /healthz probes.
package main

import (
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/dong4j/starcat-sharing-api/internal/handler"
	"github.com/dong4j/starcat-sharing-api/internal/store"
)

func main() {
	// Configuration
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "https://starcat.ink"
	}

	// Data file path: prefer STORE_FILE env var, fall back to current directory
	storeFile := os.Getenv("STORE_FILE")
	if storeFile == "" {
		storeFile = "data.json"
	}

	// Initialize store
	s, err := store.NewMemoryStore(storeFile)
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}

	// Load templates
	var templates *template.Template
	if tmpl, err := template.ParseGlob("templates/*.html"); err != nil {
		log.Fatalf("Failed to parse templates: %v", err)
	} else {
		templates = tmpl
	}

	// Initialize handler
	shareHandler := handler.NewShareHandler(s, templates, baseURL)

	// Register routes (Go 1.22+ style: custom mux + method-aware paths)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/share", shareHandler.HandleCreateShare)
	mux.HandleFunc("GET /s/{id}", shareHandler.HandleViewShare)
	mux.HandleFunc("GET /healthz", healthzHandler)

	// Configuration: PORT env var
	port := os.Getenv("PORT")
	if port == "" {
		port = "5001"
	}

	// Graceful shutdown on SIGINT / SIGTERM
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Received shutdown signal, closing service...")
		os.Exit(0)
	}()

	// Start HTTP server
	log.Printf("starcat-sharing-api starting on port %s", port)
	log.Printf("Endpoints:")
	log.Printf("  POST /api/share    - Create share link")
	log.Printf("  GET  /s/{id}       - View share page")
	log.Printf("  GET  /healthz      - Health check")
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// healthzHandler health check (used by Fly.io http_service.checks)
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
