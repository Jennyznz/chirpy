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
	"github.com/Jennyznz/chirpy.git/internal/auth"
)

func main() {
	godotenv.Load()
	platform := os.Getenv("PLATFORM")
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if (err != nil) {
		log.Printf("Error loading database: %s", err)
	}
	dbQueries := database.New(db)

	serveMux := http.NewServeMux()
	fullDir := http.Dir(".")
	strippedHandler := http.StripPrefix("/app", http.FileServer(fullDir))

	apiConfig_1 := &apiConfig{
		fileserverHits: atomic.Int32{},
		db: dbQueries,
		platform: platform,
		jwtSecret: os.Getenv("JWT_SECRET"),
	}
	
	serveMux.Handle("/app/", apiConfig_1.middlewareMetricsInc(strippedHandler))
	serveMux.HandleFunc("GET /api/healthz", readinessEndpoints)
	serveMux.HandleFunc("GET /admin/metrics", apiConfig_1.getNumHits)
	serveMux.HandleFunc("POST /api/users", apiConfig_1.createUser)	// createUser will need access to the existing db, and other config info
	serveMux.HandleFunc("POST /admin/reset", apiConfig_1.reset)
	serveMux.HandleFunc("POST /api/chirps", apiConfig_1.createChirp)
	serveMux.HandleFunc("GET /api/chirps", apiConfig_1.getAllChirps)
	serveMux.HandleFunc("GET /api/chirps/{chirpID}", apiConfig_1.getChirp)
	serveMux.HandleFunc("POST /api/login", apiConfig_1.login)
	serveMux.HandleFunc("POST /api/refresh", apiConfig_1.refresh)
	serveMux.HandleFunc("POST /api/revoke", apiConfig_1.revoke)
	serveMux.HandleFunc("PUT /api/users", apiConfig_1.updateUser)
	serveMux.HandleFunc("DELETE /api/chirps/{chirpID}", apiConfig_1.deleteChirp)
	serveMux.HandleFunc("POST /api/polka/webhooks", apiConfig_1.upgradeUser)

	server := &http.Server {
		Handler: serveMux,
		Addr: ":8080",
	}

	server.ListenAndServe()
}

func (cfg *apiConfig) upgradeUser(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Event string `json:"event"`
		Data struct {
			UserID uuid.UUID `json:"user_id"`
		}
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if (err != nil) {
		log.Printf("Invalid request body: %s", err)
		w.WriteHeader(500)
		return
	}

	if params.Event != "user.upgraded" {
		log.Printf("Invalid event. Must be 'user.upgraded'")
		w.WriteHeader(204)
		return
	} else {
		err := cfg.db.UpgradeUser(r.Context(), params.Data.UserID)
		if err != nil {
			log.Printf("User ID not found, %s", err)
			w.WriteHeader(404)
			return
		}

		w.WriteHeader(204)
		return
	}
}

func (cfg *apiConfig) deleteChirp(w http.ResponseWriter, r *http.Request) {
	// Get chirp ID
	chirpIDString := r.PathValue("chirpID")
	chirpID, err := uuid.Parse(chirpIDString)
	if (err != nil) {
		log.Printf("Invalid chirp id path: %v", err)
		w.WriteHeader(http.StatusBadRequest)	// (400) Client provided an invalid UUID string
		return
	}

	// Get user ID
	headers := r.Header
	accessToken, err := auth.GetBearerToken(headers)
	if (err != nil) {
		log.Printf("Error getting bearer token: %s", err)
		w.WriteHeader(http.StatusUnauthorized) // 401
		return
	}
	userId, err := auth.ValidateJWT(accessToken, cfg.jwtSecret)
	if (err != nil) {
		log.Printf("Invalid token: %s", err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Check if user ID matches the one attached to the chirp
	chirp, err := cfg.db.GetChirp(r.Context(), chirpID)
	if (err != nil) {
		log.Printf("Chirp not found: %s", err)
		w.WriteHeader(404)
		return
	}
	
	// Delete chirp if user ids match
	if chirp.UserID == userId {
		err := cfg.db.DeleteChirp(r.Context(), chirpID)
		if (err != nil) {
			log.Printf("Error deleting chirp: %s", err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent) // 204
		return
	} else {
		log.Printf("User not authorized to delete chirp: %s", err)
		w.WriteHeader(http.StatusForbidden) // 403
		return	
	}
}


func (cfg *apiConfig) updateUser(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Password string `json:"password"`
		Email string `json:"email"`
	}

	// Decode request body
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if (err != nil) {
		log.Printf("Invalid request body: %s", err)
		w.WriteHeader(500)
		return
	}

	// Hash (possibly new) password
	pw := params.Password
	hashedPw, err := auth.HashPassword(pw)
	if (err != nil) {
		log.Printf("Error hashing password: %s", err)
		w.WriteHeader(500)
		return
	}

	// Find user's id
	headers := r.Header
	accessToken, err := auth.GetBearerToken(headers)
	if (err != nil) {
		log.Printf("Error getting bearer token: %s", err)
		w.WriteHeader(401)
		return
	}
	userId, err := auth.ValidateJWT(accessToken, cfg.jwtSecret)
	if (err != nil) {
		log.Printf("Invalid token: %s", err)
		w.WriteHeader(401)
		return
	}

	// Update user's information
	userInfo, err := cfg.db.UpdateEmailAndPassword(r.Context(), database.UpdateEmailAndPasswordParams{
		ID: userId, 
		Email: params.Email,
		HashedPassword: hashedPw,
	})
	if (err != nil) {
		log.Printf("Error updating email and password: %s", err)
		w.WriteHeader(401)
		return
	}

	type response struct{
		ID uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Email     string    `json:"email"`
		IsChirpyRed bool `json:"is_chirpy_red"`
	}

	res := response{
		ID: userInfo.ID,
		CreatedAt: userInfo.CreatedAt,
		UpdatedAt: userInfo.UpdatedAt,
		Email: userInfo.Email,
		IsChirpyRed: userInfo.IsChirpyRed,
	}

	responseJSON, err := json.Marshal(res)
	if (err != nil) {
		log.Printf("Error marshaling JSON: %s", err)
		w.WriteHeader(401)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(responseJSON)
}

func (cfg *apiConfig) revoke(w http.ResponseWriter, r *http.Request) {
	headers := r.Header
	tokenString, err := auth.GetBearerToken(headers)
	if (err != nil) {
		log.Printf("Error creating token string")
		w.WriteHeader(500)
		return
	}

	// Revoke refresh token
	cfg.db.RevokeRefreshToken(r.Context(), tokenString)

	type res struct {
		Token string `json:"token"`
	}

	response := res {
		Token: tokenString,
	}
	
	responseJSON, err := json.Marshal(response)
	if (err != nil) {
		log.Printf("Error marshaling json")
		w.WriteHeader(500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
	w.Write(responseJSON)

}


func (cfg *apiConfig) refresh(w http.ResponseWriter, r *http.Request) {
	headers := r.Header
	tokenString, err := auth.GetBearerToken(headers)
	if (err != nil) {
		log.Printf("Error creating token string")
		w.WriteHeader(500)
		return
	}

	refreshToken, err := cfg.db.GetRefreshToken(r.Context(), tokenString)
	if (err != nil) {
		log.Printf("Error fetching refresh token: %v", err)
		w.WriteHeader(401)
		return
	}

	if (refreshToken.RevokedAt.Valid) {
		log.Printf("Refresh token has been revoked: %v", err)
		w.WriteHeader(401)
		return
	}

	if (time.Now().After(refreshToken.ExpiresAt)) {
		log.Printf("Refresh token has expired: %v", err)
		w.WriteHeader(401)
		return
	}

	// Find user in database
	user, err := cfg.db.GetUserFromRefreshToken(r.Context(), refreshToken.Token)
	if (err != nil) {
		log.Printf("Error fetching user")
		w.WriteHeader(500)
		return
	}

	// Generate access token from userId
	accessToken, err := auth.MakeJWT(user.ID, cfg.jwtSecret, time.Duration(3600) * time.Second)
	if (err != nil) {
		log.Printf("Error creating JWT token")
		w.WriteHeader(500)
		return
	}

	type res struct {
		Token string `json:"token"`
	}

	response := res {
		Token: accessToken,
	}
	
	responseJSON, err := json.Marshal(response)
	if (err != nil) {
		log.Printf("Error marshaling json")
		w.WriteHeader(500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(responseJSON)

}

func (cfg *apiConfig) login(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Email string `json:"email"`
		Password string `json:"password"`
	}

	// Decode request body
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if (err != nil) {
		log.Printf("Invalid request body: %s", err)
		w.WriteHeader(500)
		return
	}

	userInfo, err := cfg.db.Login(r.Context(), params.Email)
	if (err != nil) {
		log.Printf("No user found for email: %v", err)
		w.WriteHeader(http.StatusUnauthorized)	// (401)
		return
	}

	pwMatch, err := auth.CheckPasswordHash(params.Password, userInfo.HashedPassword)
	if (err != nil) {
		log.Printf("Error: comparing passwords %v", err)
		w.WriteHeader(500)	
		return
	}
	if (!pwMatch) {
		log.Printf("Incorrect Password: %v", err)
		w.WriteHeader(http.StatusUnauthorized)	// (401)
		return
	}

	type res struct{
		ID uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Email     string    `json:"email"`
		IsChirpyRed bool `json:"is_chirpy_red"`
		Token string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}

	// Create an access token that expires in 1 hour
	accessToken, err := auth.MakeJWT(userInfo.ID, cfg.jwtSecret, time.Duration(3600) * time.Second)
	if (err != nil) {
		log.Printf("Error creating JWT token")
		w.WriteHeader(500)
	}

	// Create a refresh token that expires in 60 days
	rTkn := auth.MakeRefreshToken()
	refreshToken, err := cfg.db.CreateRefreshToken(r.Context(), database.CreateRefreshTokenParams{
		Token: rTkn,
		UserID: userInfo.ID,
	})
	if (err != nil) {
		log.Printf("Error creating refresh token")
		w.WriteHeader(500)
		return
	}
		
	response := res{
		ID: userInfo.ID,		
		CreatedAt: userInfo.CreatedAt,
		UpdatedAt: userInfo.UpdatedAt,
		Email: userInfo.Email,
		IsChirpyRed: userInfo.IsChirpyRed,
		Token: accessToken,
		RefreshToken: refreshToken.Token,
	}

	responseJSON, err := json.Marshal(response)
	if (err != nil) {
		log.Printf("Error marshaling json")
		w.WriteHeader(500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // 200
	w.Write(responseJSON)
}

func (cfg *apiConfig) getChirp(w http.ResponseWriter, r *http.Request) {
	// Get ID of chirp
	chirpIDString := r.PathValue("chirpID")
	chirpID, err := uuid.Parse(chirpIDString)
	if (err != nil) {
		log.Printf("Invalid chirp id path: %v", err)
		w.WriteHeader(http.StatusBadRequest)	// (400) Client provided an invalid UUID string
		return
	}
	
	// Get chirp
	dbChirp, err := cfg.db.GetChirp(r.Context(), chirpID)
	if (err != nil) {
		log.Printf("Error fetching chirp: %v", err)
		w.WriteHeader(http.StatusNotFound)	// (404) Client provided a correct UUID, but it was not found in the existing database
		return
	}

	// Map onto chirp struct to control json key names
	chirp := Chirp{
		ID: dbChirp.ID,
		CreatedAt: dbChirp.CreatedAt,
		UpdatedAt: dbChirp.UpdatedAt,
		Body: dbChirp.Body,
		UserID: dbChirp.UserID,
	}

	chirpJSON, err := json.Marshal(chirp)
	if (err != nil) {
		log.Printf("Error marshaling JSON: %v", err)
		w.WriteHeader(500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(chirpJSON)
	return
}

func (cfg *apiConfig) getAllChirps(w http.ResponseWriter, r *http.Request) {
	// Retrieve data from chirps table
	dbChirps, err := cfg.db.GetAllChirps(r.Context())
	if (err != nil) {
		log.Printf("Error fetching chirps: %v", err)
		w.WriteHeader(500)
		return
	}

	chirps := []Chirp{}

	for _, dbChirp := range dbChirps {
		chirps = append(chirps, Chirp {
			ID: dbChirp.ID,
			CreatedAt: dbChirp.CreatedAt,
			UpdatedAt: dbChirp.UpdatedAt,
			Body: dbChirp.Body,
			UserID: dbChirp.UserID,
		})
	}

	chirpsJSON, err := json.Marshal(chirps)
	if (err != nil) {
		log.Printf("Error marshaling JSON: %v", err)
		w.WriteHeader(500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(chirpsJSON)
}

func (cfg *apiConfig) createChirp(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Body string `json:"body"`
		UserID uuid.UUID `json:"user_id"`
	}

	type errorResponse struct {
		Error string `json:"error"`
	}

	// Get bearer token from request header
	headers := r.Header
	tokenString, err := auth.GetBearerToken(headers)
	if (err != nil) {
		log.Printf("Error getting token string: %s", err)
		w.WriteHeader(500)
		return
	}

	// Check if token string is valid
	userID, err := auth.ValidateJWT(tokenString, cfg.jwtSecret)
	if (err != nil) {
		log.Printf("Error validating JWT: %s", err)
		w.WriteHeader(http.StatusUnauthorized) // (401)
		return
	}

	// Decode request body
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err = decoder.Decode(&params)
	if (err != nil) {
		log.Printf("Invalid request body: %s", err)
		res := errorResponse{
			Error: "Something went wrong",
		}
		data, errMsg := json.Marshal(res)
		if (errMsg != nil) {
			log.Printf("Error marshaling json: %s", errMsg)
			w.WriteHeader(500)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write(data)
		return
	}

	// Check that chirp body is under 140 characters
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

	// Censor profane words in chirp body
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

	// Create a new chirp
	newChirp, err := cfg.db.CreateChirp(r.Context(), database.CreateChirpParams{
		Body: cleanedBody,
		UserID: userID,
	})
	if (err != nil) {
		log.Printf("Error creating new chirp: %v", err)
		res := errorResponse{
			Error: "Something went wrong",
		}
		data, errMsg := json.Marshal(res)
		if (errMsg != nil) {
			log.Printf("Error marshaling json: %s", errMsg)
			w.WriteHeader(500)
			return
		} 
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write(data)
		return
	}

	chirp := Chirp{
		ID: newChirp.ID,		
		CreatedAt: newChirp.CreatedAt,
		UpdatedAt: newChirp.UpdatedAt,
		Body: newChirp.Body,
		UserID: newChirp.UserID,	// snakecase converted to pascal case in the Go code produced by sqlc
	}

	chirpJSON, err := json.Marshal(chirp)
	if (err != nil) {
		log.Printf("Error marshaling json")
		w.WriteHeader(500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated) // 201
	w.Write(chirpJSON)

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
		Password string `json: password`
	}

	type errorResponse struct {
		Error string `json:"error"`
	}

	// Decode the email that arrives inside the request body
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if (err != nil) {
		log.Printf("Invalid request body: %s", err)
		res := errorResponse{
			Error: "Something went wrong",
		}
		data, errMsg := json.Marshal(res)
		if (errMsg != nil) {
			log.Printf("Error marshaling json: %s", errMsg)
			w.WriteHeader(500)
			return
		} 
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write(data)
		return
	}

	hash, err := auth.HashPassword(params.Password)
	if (err != nil) {
		log.Printf("Error hashing password %v", err)
		w.WriteHeader(500)	
		return
	}

	// Create a new user
	newUser, err := cfg.db.CreateUser(r.Context(), database.CreateUserParams{
		Email: params.Email,
		HashedPassword: hash,
	})
	if (err != nil) {
		log.Printf("Error creating new user: %s", err)
		res := errorResponse{
			Error: "Something went wrong",
		}
		data, errMsg := json.Marshal(res)
		if (errMsg != nil) {
			log.Printf("Error marshaling json: %s", errMsg)
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
		IsChirpyRed: newUser.IsChirpyRed,
	}

	// Encode user information as JSON in response
	userJSON, err := json.Marshal(user)
	if (err != nil) {
		log.Printf("Error encoding JSON in response: %s", err)
		res := errorResponse{
			Error: "Something went wrong",
		}
		data, errMsg := json.Marshal(res)
		if (errMsg != nil) {
			log.Printf("Error marshaling json: %s", errMsg)
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
	jwtSecret string
}

type User struct {
	ID uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
	IsChirpyRed bool `json:"is_chirpy_red"`
	HashedPassword string `json: "password"`
}

type Chirp struct {
	ID uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body string `json:"body"`
	UserID uuid.UUID `json:"user_id"`
}


