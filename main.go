package main

import _ "github.com/lib/pq"

import (
	"net/http"
	"sync/atomic"
	"fmt"
	"log"
	"encoding/json"
	"strings"
	"os"
	"database/sql"
	"github.com/Jennyznz/chirpy.git/internal/database"
	"github.com/joho/godotenv"
	"github.com/google/uuid"
	"time"
)

func main() {
	godotenv.Load()
	platform := os.Getenv("PLATFORM")
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if (err != nil) {
		log.Printf("Error loading database", err)
	}
	dbQueries := database.New(db)

	serveMux := http.NewServeMux()
	fullDir := http.Dir(".")
	strippedHandler := http.StripPrefix("/app", http.FileServer(fullDir))

	apiConfig_1 := &apiConfig{
		fileserverHits: atomic.Int32{},
		db: dbQueries,
		platform: platform,
	}
	
	serveMux.Handle("/app/", apiConfig_1.middlewareMetricsInc(strippedHandler))
	serveMux.HandleFunc("GET /api/healthz", readinessEndpoints)
	serveMux.HandleFunc("GET /admin/metrics", apiConfig_1.getNumHits)
	serveMux.HandleFunc("POST /api/validate_chirp", validateChirp)
	serveMux.HandleFunc("POST /api/users", apiConfig_1.createUser)	// createUser will need access to the existing db, and other config info
	serveMux.HandleFunc("POST /admin/reset", apiConfig_1.reset)

	server := &http.Server {
		Handler: serveMux,
		Addr: ":8080",
	}

	server.ListenAndServe()
}

func (cfg *apiConfig) reset(w http.ResponseWriter, r *http.Request) {
	// Return with error if not in dev mode
	if (cfg.platform != "dev") {
		log.Printf("Must be in dev mode to reset users")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Reset hits counter
	cfg.fileserverHits.Store(0); 
	// Reset users
	cfg.db.ResetUsers(r.Context())

	// Response
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	return
}


func (cfg *apiConfig) createUser(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Email string `json:"email"`	// json.Decoder can only populated exported struct fields. Lowercased fields are left as empty strings
	}

	type errorResponse struct {
		Error string `json:"error"`
	}

	// Decode the email that arrives inside the request body
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if (err != nil) {
		log.Printf("Invalid request body", err)
		res := errorResponse{
			Error: "Something went wrong",
		}
		data, errMsg := json.Marshal(res)
		if (errMsg != nil) {
			log.Printf("Error marshaling json", errMsg)
			w.WriteHeader(500)
			return
		} 
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write(data)
		return
	}


	// Create a new user
	newUser, err := cfg.db.CreateUser(r.Context(), params.Email)
	if (err != nil) {
		log.Printf("Error creating new user", err)
		res := errorResponse{
			Error: "Something went wrong",
		}
		data, errMsg := json.Marshal(res)
		if (errMsg != nil) {
			log.Printf("Error marshaling json", errMsg)
			w.WriteHeader(500)
			return
		} 
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write(data)
		return
	}

	// Map new user information onto User struct to control JSON keys
	user := User{
		ID: newUser.ID,
		CreatedAt: newUser.CreatedAt,
		UpdatedAt: newUser.UpdatedAt,
		Email: newUser.Email,
	}

	// Encode user information as JSON in response
	userJSON, err := json.Marshal(user)
	if (err != nil) {
		log.Printf("Error encoding JSON in response", err)
		res := errorResponse{
			Error: "Something went wrong",
		}
		data, errMsg := json.Marshal(res)
		if (errMsg != nil) {
			log.Printf("Error marshaling json", errMsg)
			w.WriteHeader(500)
			return
		} 
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write(data)
		return
	}


	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write(userJSON)
}

func validateChirp(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Body string `json:"body"`
	}

	type validResponse struct {
		Body string `json:"cleaned_body"`
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
		data, errMsg := json.Marshal(res)
		if (errMsg != nil) {
			log.Printf("Error marshaling json", errMsg)
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

	words := strings.Split(params.Body, " ")
	profane := []string{"kerfuffle", "sharbert", "fornax"}
	var cleanedWords []string

	for _, word := range words {
		lcWord := strings.ToLower(word)
		isProfane := false
		for _, p := range profane {
			if (lcWord == p) {
				cleanedWords = append(cleanedWords, "****")
				isProfane = true
				break
			}
		}

		if (!isProfane) {
			cleanedWords = append(cleanedWords, word)
		}
	}

	cleanedBody := strings.Join(cleanedWords, " ")

	validRes := validResponse{
		Body: cleanedBody,
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

type apiConfig struct {
	fileserverHits atomic.Int32	// stdlib that safety increments and reads ints across goroutines
	db *database.Queries
	platform string
}

type User struct {
	ID uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}


