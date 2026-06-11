package main

import (
	"net/http"
	"sync/atomic"
	"fmt"
)

func main() {
	serveMux := http.NewServeMux()
	fullDir := http.Dir(".")
	strippedHandler := http.StripPrefix("/app", http.FileServer(fullDir))
	apiConfig_1 := &apiConfig{}
	
	serveMux.Handle("/app/", apiConfig_1.middlewareMetricsInc(strippedHandler))
	serveMux.HandleFunc("/healthz", readinessEndpoints)
	serveMux.HandleFunc("/metrics", apiConfig_1.getNumHits)
	serveMux.HandleFunc("/reset", apiConfig_1.resetHits)

	server := &http.Server {
		Handler: serveMux,
		Addr: ":8080",
	}

	server.ListenAndServe()
}

func readinessEndpoints(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {	// Handler as parameter and return statement because middleware
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {	// Runs every time after the first call to middlewareMetricsInc
		cfg.fileserverHits.Add(int32(1))
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) getNumHits(w http.ResponseWriter, r *http.Request) {	// http.ServeMux needs every handler to accept a http writer and reader, even if not used
	result := fmt.Sprintf("Hits: %d", cfg.fileserverHits.Load())
	w.Write([]byte(result))
}

func(cfg *apiConfig) resetHits(w http.ResponseWriter, r *http.Request) { 
	cfg.fileserverHits.Store(0); 
}

type apiConfig struct {
	fileserverHits atomic.Int32	// stdlib that safety increments and reads ints across goroutines
}


