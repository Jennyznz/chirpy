package main

import (
	"net/http"
	"sync/atomic"
	"fmt"
	"log"
	"encoding/json"
)

func main() {
	serveMux := http.NewServeMux()
	fullDir := http.Dir(".")
	strippedHandler := http.StripPrefix("/app", http.FileServer(fullDir))
	apiConfig_1 := &apiConfig{}
	
	serveMux.Handle("/app/", apiConfig_1.middlewareMetricsInc(strippedHandler))
	serveMux.HandleFunc("GET /api/healthz", readinessEndpoints)
	serveMux.HandleFunc("GET /admin/metrics", apiConfig_1.getNumHits)
	serveMux.HandleFunc("POST /admin/reset", apiConfig_1.resetHits)
	serveMux.HandleFunc("POST /api/validate_chirp", validateChirp)

	server := &http.Server {
		Handler: serveMux,
		Addr: ":8080",
	}

	server.ListenAndServe()
}

func validateChirp(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Body string `json:"body"`
	}

	type validResponse struct {
		Valid bool `json:"valid"`
	}

	type errorResponse struct {
		Error string `json:"error"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if (err != nil) {
		log.Printf("Error decoding request parameters", err)
		res := errorResponse{
			Error: "Something went wrong",
		}
		data, error := json.Marshal(res)
		if (error != nil) {
			log.Printf("Error marshaling json", error)
			w.WriteHeader(500)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write(data)
		return
	}

	if (len(params.Body) > 140) {
		res := errorResponse{
			Error: "Chirp is too long",
		}
		data, err := json.Marshal(res)
		if (err != nil) {
			log.Printf("Error marshaling json")
			w.WriteHeader(500)
			return
		}

		log.Printf("Chirp is too long")
		w.WriteHeader(400)
		w.Write(data)
		return
	}

	validRes := validResponse{
		Valid: true,
	}

	data, err := json.Marshal(validRes)
	if (err != nil) {
		log.Printf("Error marshaling json")
		w.WriteHeader(500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write([]byte(data))
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
	result := fmt.Sprintf(`
		<html>
			<body>
				<h1>Welcome, Chirpy Admin</h1>
				<p>Chirpy has been visited %d times!</p>
			</body>
		</html>
	`, cfg.fileserverHits.Load())
	w.Header().Add("Content-Type", "text/html") // Must go before .Write()
	w.Write([]byte(result))
}

func(cfg *apiConfig) resetHits(w http.ResponseWriter, r *http.Request) { 
	cfg.fileserverHits.Store(0); 
}

type apiConfig struct {
	fileserverHits atomic.Int32	// stdlib that safety increments and reads ints across goroutines
}


