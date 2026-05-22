package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/iheartmywife/chirpy/internal/database"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
	platform       string
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

// JSON FUNCS
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
	mux.HandleFunc("POST /api/chirps", func(w http.ResponseWriter, r *http.Request) {

		//decode json
		type paramaters struct {
			Body    string    `json:"body"`
			User_id uuid.UUID `json:"user_id"`
		}
		decoder := json.NewDecoder(r.Body)
		params := paramaters{}
		payload := database.CreateChirpParams{}
		err := decoder.Decode(&params)
		if err != nil {
			apiConfig.respondWithError(w, http.StatusBadRequest, "Something went wrong") //400
			return
		}
		payload.Body = params.Body
		payload.UserID = params.User_id
		if len(payload.Body) > 140 {
			apiConfig.respondWithError(w, http.StatusBadRequest, "Chirp is too long") //400
			return
		}
		payload.Body = ReplaceProfanity(payload.Body)
		type chirpPayload struct {
			ID        uuid.UUID `json:"id"`
			CreatedAt time.Time `json:"created_at"`
			UpdatedAt time.Time `json:"updated_at"`
			Body      string    `json:"body"`
			User_id   uuid.UUID `json:"user_id"`
		}
		newChirp, err := apiConfig.dbQueries.CreateChirp(r.Context(), payload)
		if err != nil {
			apiConfig.respondWithError(w, http.StatusInternalServerError, "Internal Server Error") //500
			return
		}
		formattedNewChirp := chirpPayload{
			ID:        newChirp.ID,
			CreatedAt: newChirp.CreatedAt,
			UpdatedAt: newChirp.UpdatedAt,
			Body:      newChirp.Body,
			User_id:   newChirp.UserID,
		}
		apiConfig.respondWithJSON(w, http.StatusCreated, formattedNewChirp) //201
	})
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("POST /api/users", func(w http.ResponseWriter, r *http.Request) {
		type email struct {
			Email string `json:"email"`
		}
		decoder := json.NewDecoder(r.Body)
		userEmail := email{}
		err := decoder.Decode(&userEmail)
		if err != nil {
			log.Print("error decoding user email")
			return
		}
		newCreatedUser, err := apiConfig.dbQueries.CreateUser(r.Context(), userEmail.Email)
		if err != nil {
			log.Print("error creating new user in DB")
		}
		type User struct {
			ID        uuid.UUID `json:"id"`
			CreatedAt time.Time `json:"created_at"`
			UpdatedAt time.Time `json:"updated_at"`
			Email     string    `json:"email"`
		}
		user := User{
			ID:        newCreatedUser.ID,
			CreatedAt: newCreatedUser.CreatedAt,
			UpdatedAt: newCreatedUser.UpdatedAt,
			Email:     newCreatedUser.Email,
		}
		apiConfig.respondWithJSON(w, 201, user)
	})
	mux.HandleFunc("POST /api/validate_chirp", func(w http.ResponseWriter, r *http.Request) {

	})
	serverErr := srvr.ListenAndServe()
	if serverErr != nil {
		log.Fatal(serverErr)
	}
}
