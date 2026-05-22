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

	"github.com/iheartmywife/chirpy/internal/database"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
}

// PROFANITY HANDLERS
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
func (cfg *apiConfig) ResetMetricsInc(w http.ResponseWriter, r *http.Request) {
	cfg.fileserverHits.Store(0)
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
		log.Fatal("error loading new .env file")
	}
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("error generating new db")
	}
	apiConfig := apiConfig{}
	apiConfig.dbQueries = database.New(db)
	mux := http.NewServeMux()
	mux.Handle("/app/", apiConfig.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(".")))))
	srvr := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /admin/metrics", apiConfig.AdminMetrics)
	mux.HandleFunc("POST /admin/reset", apiConfig.ResetMetricsInc)
	mux.HandleFunc("POST /api/validate_chirp", func(w http.ResponseWriter, r *http.Request) {
		//define struct
		type parameters struct {
			Body         string `json:"body"`
			Cleaned_Body string `json:"cleaned_body"`
		}
		//decode json
		decoder := json.NewDecoder(r.Body)
		params := parameters{}
		err := decoder.Decode(&params)
		if err != nil {
			apiConfig.respondWithError(w, 400, "Something went wrong")
			return
		}
		log.Printf("params: %+v", params)
		if len(params.Body) > 140 {
			apiConfig.respondWithError(w, 400, "Chirp is too long")
			return
		}
		log.Printf("params: %+v", params)
		params.Cleaned_Body = ReplaceProfanity(params.Body)
		log.Printf("params: %+v", params)
		apiConfig.respondWithJSON(w, 200, params)
	})
	serverErr := srvr.ListenAndServe()
	if serverErr != nil {
		log.Fatal(serverErr)
	}
}
