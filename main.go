package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/iheartmywife/chirpy/internal/auth"
	"github.com/iheartmywife/chirpy/internal/database"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

const MaxChirpLength = 140

type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
	platform       string
}

type chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}
type userData struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

// internal/database to json converters
func databaseChirpToChirp(c database.Chirp) chirp {
	return chirp{
		ID:        c.ID,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
		Body:      c.Body,
		UserID:    c.UserID,
	}
}

func databaseUserToUser(u database.User) User {
	return User{
		ID:        u.ID,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
		Email:     u.Email,
	}
}

// CHIRP VALIDATION/PROFANITY HANDLERS
func ReplaceProfanity(chirp string) string {
	contents := strings.Fields(chirp)
	replacementWord := "****"
	for i, word := range contents {
		if CheckForProfanity(word) == true {
			contents[i] = strings.Replace(word, word, replacementWord, 1)
		}
	}
	return strings.Join(contents, " ")
}

func CheckForProfanity(word string) bool {
	values := []string{"kerfuffle", "sharbert", "fornax"}

	found := false
	for _, v := range values {
		if strings.ToLower(word) == v {
			found = true
			break
		}
	}
	return found
}

func ValidateChirp(body string) (string, error) {
	if len(body) > MaxChirpLength {
		return "", errors.New("Chirp is too long")
	}
	return ReplaceProfanity(body), nil
}

// Auth Help
func (cfg *apiConfig) DecodeLoginInfo(w http.ResponseWriter, r *http.Request) (data userData) {
	decoder := json.NewDecoder(r.Body)
	userInput := userData{}
	err := decoder.Decode(&userInput)
	if err != nil {
		cfg.respondWithError(w, http.StatusUnauthorized, "error decoding json")
		return userData{}
	}
	return userInput

}

// JSON FUNCS
func (cfg *apiConfig) respondWithErrorSpecific(w http.ResponseWriter, code int, msg string, e error) {
	type returnVals struct {
		Err string `json:"error"`
	}
	respBody := returnVals{
		Err: fmt.Sprintf(msg, e),
	}
	data, er := json.Marshal(respBody)
	if er != nil {
		log.Printf("error marshalling Json: %s", er)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(data)
}

func (cfg *apiConfig) respondWithError(w http.ResponseWriter, code int, msg string) {
	type returnVals struct {
		Err string `json:"error"`
	}
	respBody := returnVals{
		Err: msg,
	}
	data, er := json.Marshal(respBody)
	if er != nil {
		log.Printf("error marshalling Json: %s", er)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(data)
}

func (cfg *apiConfig) respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	data, er := json.Marshal(payload)
	if er != nil {
		log.Printf("error marshalling Json: %s", er)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(data)
}

// METRICS
func (cfg *apiConfig) CreateChirp(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Body   string    `json:"body"`
		UserID uuid.UUID `json:"user_id"`
	}
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "couldn't decode parameters") //400
		return
	}
	validated, err := ValidateChirp(params.Body)
	if err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload := database.CreateChirpParams{
		Body:   validated,
		UserID: params.UserID,
	}
	newChirp, err := cfg.dbQueries.CreateChirp(r.Context(), payload)
	if err != nil {
		cfg.respondWithError(w, http.StatusInternalServerError, "could not create chirp") //500
		return
	}
	formattedNewChirp := databaseChirpToChirp(newChirp)
	cfg.respondWithJSON(w, http.StatusCreated, formattedNewChirp) //201
}
func (cfg *apiConfig) GetAllChirps(w http.ResponseWriter, r *http.Request) {
	var allChirpsFormatted []chirp

	allChirpsRaw, err := cfg.dbQueries.GetAllChirps(r.Context())
	if err != nil {
		cfg.respondWithError(w, http.StatusInternalServerError, "could not get all chirps")
	}
	for _, chirp := range allChirpsRaw {
		formattedChirp := databaseChirpToChirp(chirp)
		allChirpsFormatted = append(allChirpsFormatted, formattedChirp)
	}
	cfg.respondWithJSON(w, http.StatusOK, allChirpsFormatted)

}
func (cfg *apiConfig) GetChirp(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("chirpID"))
	if err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "could not parse chirpID")
		return
	}
	fullRecord, err := cfg.dbQueries.GetChirp(r.Context(), id)
	if err != nil {
		cfg.respondWithError(w, http.StatusNotFound, "could not find Chirp")
		return
	}
	formattedChirp := databaseChirpToChirp(fullRecord)
	cfg.respondWithJSON(w, http.StatusOK, formattedChirp)
}

func (cfg *apiConfig) ResetMetrics(w http.ResponseWriter, r *http.Request) {
	if cfg.platform == "dev" {
		err := cfg.dbQueries.DeleteAllUsers(r.Context())
		if err != nil {
			log.Printf("failed to delete db: %v", err)
			w.WriteHeader(http.StatusInternalServerError) //500
			return
		}
		cfg.fileserverHits.Store(0)
		w.WriteHeader(http.StatusOK) //200

	} else {
		w.WriteHeader(http.StatusForbidden) //403
	}
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) getRawHits() int {
	return int(cfg.fileserverHits.Load())
}

func (cfg *apiConfig) PrintHits(w http.ResponseWriter, r *http.Request) {
	hits := cfg.getRawHits()
	myStr := fmt.Sprintf("Hits: %d", hits)
	w.Write([]byte(myStr))
}

func (cfg *apiConfig) AdminMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, cfg.getRawHits())))
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Print("error loading new .env file")
	}
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Print("error generating new db")
	}
	apiConfig := apiConfig{}
	apiConfig.platform = os.Getenv("PLATFORM")
	apiConfig.dbQueries = database.New(db)
	mux := http.NewServeMux()
	mux.Handle("/app/", apiConfig.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(".")))))
	srvr := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	mux.HandleFunc("GET /admin/metrics", apiConfig.AdminMetrics)
	mux.HandleFunc("POST /admin/reset", apiConfig.ResetMetrics)
	mux.HandleFunc("POST /api/chirps", apiConfig.CreateChirp)
	mux.HandleFunc("GET /api/chirps", apiConfig.GetAllChirps)
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiConfig.GetChirp)
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		loginInfo := apiConfig.DecodeLoginInfo(w, r)
		user, err := apiConfig.dbQueries.GetUser(r.Context(), loginInfo.Email)
		if err != nil {
			apiConfig.respondWithError(w, http.StatusUnauthorized, "Incorrect email or password, error with getting user")
			return
		}
		valid, err := auth.CheckPasswordHash(loginInfo.Password, user.HashedPassword)
		if err != nil {
			apiConfig.respondWithErrorSpecific(w, http.StatusUnauthorized, "Incorrect email or password, error in checking password hash: %v", err)
			return
		}
		if !valid {
			apiConfig.respondWithError(w, http.StatusUnauthorized, "Incorrect email or password, password does not match hash")
			return
		}
		formattedUser := databaseUserToUser(user)
		apiConfig.respondWithJSON(w, http.StatusOK, formattedUser)

	})
	mux.HandleFunc("POST /api/users", func(w http.ResponseWriter, r *http.Request) {
		userInput := apiConfig.DecodeLoginInfo(w, r)
		hashedPassword, err := auth.HashPassword(userInput.Password)
		if err != nil {
			log.Print("error hashing password")
		}
		newCreatedUser, err := apiConfig.dbQueries.CreateUser(r.Context(), database.CreateUserParams{
			Email:          userInput.Email,
			HashedPassword: hashedPassword,
		})
		if err != nil {
			log.Print("error creating new user in DB")
		}
		user := databaseUserToUser(newCreatedUser)
		apiConfig.respondWithJSON(w, 201, user)
	})
	mux.HandleFunc("POST /api/validate_chirp", func(w http.ResponseWriter, r *http.Request) {

	})
	serverErr := srvr.ListenAndServe()
	if serverErr != nil {
		log.Fatal(serverErr)
	}
}
