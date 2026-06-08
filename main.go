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
	jwtSecret      string
}

type chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}
type userData struct {
	Email     string        `json:"email"`
	Password  string        `json:"password"`
	ExpiresIn time.Duration `json:"expires_in_seconds"`
}
type User struct {
	ID           uuid.UUID `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Email        string    `json:"email"`
	Token        string    `json:"token"`
	RefreshToken string    `json:"refresh_token"`
	IsChirpyRed  bool      `json:"is_chirpy_red"`
}

type webHookPayload struct {
	Event string `json:"event"`
	Data  struct {
		UserID uuid.UUID `json:"user_id"`
	} `json:"data"`
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

func databaseUserToUser(id uuid.UUID, createdAt time.Time, updatedAt time.Time, email string, is_chirpy_red bool) User {
	return User{
		ID:          id,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		Email:       email,
		IsChirpyRed: is_chirpy_red,
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

// USER METHODS
func (cfg *apiConfig) UpdateUserLogin(w http.ResponseWriter, r *http.Request) {
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		cfg.respondWithErrorSpecific(w, http.StatusUnauthorized, "No bearer token", err) //401
		return
	}
	NewInfo, err := Decode[userData](r)
	if err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "Json decoding failed")
	}

	id, er := auth.ValidateJWT(token, cfg.jwtSecret)
	if er != nil {
		cfg.respondWithErrorSpecific(w, http.StatusUnauthorized, "unable to validate jwt", er) //401
		return
	}
	hashedPW, e := auth.HashPassword(NewInfo.Password)
	if e != nil {
		cfg.respondWithErrorSpecific(w, http.StatusUnauthorized, "unable to hash password", e) //401
		return
	}
	dbUpdatedUser, err := cfg.dbQueries.UpdateUser(r.Context(), database.UpdateUserParams{
		ID:             id,
		Email:          NewInfo.Email,
		HashedPassword: hashedPW,
	})
	if err != nil {
		cfg.respondWithErrorSpecific(w, http.StatusUnauthorized, "Unable to update user", err) //401
		return
	}
	user := databaseUserToUser(dbUpdatedUser.ID, dbUpdatedUser.CreatedAt, dbUpdatedUser.UpdatedAt, dbUpdatedUser.Email, dbUpdatedUser.IsChirpyRed)
	cfg.respondWithJSON(w, http.StatusOK, user)
}

func (cfg *apiConfig) CreateUser(w http.ResponseWriter, r *http.Request) {
	userInput, err := Decode[userData](r)
	if err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "Json decoding failed")
	}
	hashedPassword, err := auth.HashPassword(userInput.Password)
	if err != nil {
		log.Print("error hashing password")
	}
	newCreatedUser, err := cfg.dbQueries.CreateUser(r.Context(), database.CreateUserParams{
		Email:          userInput.Email,
		HashedPassword: hashedPassword,
	})
	if err != nil {
		log.Print("error creating new user in DB")
	}
	user := databaseUserToUser(newCreatedUser.ID, newCreatedUser.CreatedAt, newCreatedUser.UpdatedAt, newCreatedUser.Email, newCreatedUser.IsChirpyRed)
	cfg.respondWithJSON(w, 201, user)
}
func (cfg *apiConfig) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	webHookPayload, err := Decode[webHookPayload](r)
	if err != nil {
		cfg.respondWithErrorSpecific(w, http.StatusInternalServerError, "error decoding json", err)
	}
	if webHookPayload.Event != "user.upgraded" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_, er := cfg.dbQueries.UpdateChirpyRedStatus(r.Context(), webHookPayload.Data.UserID)
	if er != nil {
		if er == sql.ErrNoRows {
			cfg.respondWithError(w, http.StatusNotFound, "User not found")
		} else {
			cfg.respondWithError(w, http.StatusInternalServerError, "internal database error")
		}
	}
	cfg.respondWithJSON(w, http.StatusNoContent, r.Body)

}

func Decode[T any](r *http.Request) (T, error) {
	var out T
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&out)
	return out, err
}

// Authorization Help

// Authorization
func (cfg *apiConfig) Login(w http.ResponseWriter, r *http.Request) {
	loginInfo, err := Decode[userData](r)
	if err != nil {
		cfg.respondWithErrorSpecific(w, http.StatusInternalServerError, "error decoding json", err)
	}
	if loginInfo.ExpiresIn == 0 {
		loginInfo.ExpiresIn = time.Hour
	}
	user, err := cfg.dbQueries.GetUser(r.Context(), loginInfo.Email)
	if err != nil {
		cfg.respondWithError(w, http.StatusUnauthorized, "Incorrect email or password, error with getting user")
		return
	}
	valid, err := auth.CheckPasswordHash(loginInfo.Password, user.HashedPassword)
	if err != nil {
		cfg.respondWithErrorSpecific(w, http.StatusUnauthorized, "Incorrect email or password, error in checking password hash: %v", err)
		return
	}
	if !valid {
		cfg.respondWithError(w, http.StatusUnauthorized, "Incorrect email or password, password does not match hash")
		return
	}
	formattedUser := databaseUserToUser(user.ID, user.CreatedAt, user.UpdatedAt, user.Email, user.IsChirpyRed)
	formattedUser.Token, err = auth.MakeJWT(formattedUser.ID, cfg.jwtSecret)
	if err != nil {
		cfg.respondWithError(w, http.StatusInternalServerError, "Error creating auth token")
		return
	}
	refreshToken, err := cfg.dbQueries.CreateRefreshToken(r.Context(), database.CreateRefreshTokenParams{
		Token:     auth.MakeRefreshToken(),
		UserID:    formattedUser.ID,
		ExpiresAt: time.Now().AddDate(0, 0, 60),
	})
	formattedUser.RefreshToken = refreshToken.Token
	cfg.respondWithJSON(w, http.StatusOK, formattedUser)
}

func (cfg *apiConfig) RefreshToken(w http.ResponseWriter, r *http.Request) {
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		cfg.respondWithError(w, http.StatusUnauthorized, err.Error()) //401
		return
	}
	user, err := cfg.dbQueries.GetUserFromRefreshToken(r.Context(), token)
	if err != nil {
		cfg.respondWithErrorSpecific(w, http.StatusUnauthorized, "error found", err)
		return
	}
	formattedUser := databaseUserToUser(user.ID, user.CreatedAt, user.UpdatedAt, user.Email, user.IsChirpyRed)
	newAccessToken, er := auth.MakeJWT(formattedUser.ID, cfg.jwtSecret)
	if er != nil {
		cfg.respondWithErrorSpecific(w, http.StatusBadRequest, "error: ", er)
		return
	}
	formattedUser.Token = newAccessToken
	cfg.respondWithJSON(w, http.StatusOK, formattedUser)
}

func (cfg *apiConfig) RevokeToken(w http.ResponseWriter, r *http.Request) {
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		cfg.respondWithError(w, http.StatusUnauthorized, err.Error()) //401
		return
	}
	cfg.dbQueries.RevokeToken(r.Context(), token)
	w.WriteHeader(http.StatusNoContent)
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

// CHIRPS
func (cfg *apiConfig) CreateChirp(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Body string `json:"body"`
	}
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, "couldn't decode parameters") //400
		return
	}
	//potentially abstract away into helper func?
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		cfg.respondWithError(w, http.StatusUnauthorized, err.Error()) //401
		return
	}
	id, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		cfg.respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	validChirp, err := ValidateChirp(params.Body)
	if err != nil {
		cfg.respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload := database.CreateChirpParams{
		Body:   validChirp,
		UserID: id,
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
func (cfg *apiConfig) DeleteChirp(w http.ResponseWriter, r *http.Request) {
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		cfg.respondWithErrorSpecific(w, http.StatusUnauthorized, "No bearer token", err) //403
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		cfg.respondWithErrorSpecific(w, http.StatusForbidden, "unable to validate token", err) //403
		return
	}
	chirpID, _ := uuid.Parse(r.PathValue("chirpID"))
	chirp, err := cfg.dbQueries.GetChirp(r.Context(), chirpID)
	if err != nil {
		cfg.respondWithErrorSpecific(w, http.StatusForbidden, "chirp does not exist", err) //403
		return
	}
	if chirp.UserID == userID {
		cfg.dbQueries.DeleteChirp(r.Context(), database.DeleteChirpParams{
			ID:     chirp.ID,
			UserID: chirp.UserID,
		})
		w.WriteHeader(http.StatusNoContent)
		return
	}
	cfg.respondWithError(w, http.StatusForbidden, "chirp does not belong to user")
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
	apiConfig.jwtSecret = os.Getenv("SECRET")
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
	mux.HandleFunc("DELETE /api/chirps/{chirpID}", apiConfig.DeleteChirp)
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("POST /api/login", apiConfig.Login)
	mux.HandleFunc("POST /api/polka/webhooks", apiConfig.HandleWebhook)
	mux.HandleFunc("POST /api/refresh", apiConfig.RefreshToken)
	mux.HandleFunc("POST /api/revoke", apiConfig.RevokeToken)
	mux.HandleFunc("POST /api/users", apiConfig.CreateUser)
	mux.HandleFunc("PUT /api/users", apiConfig.UpdateUserLogin)
	serverErr := srvr.ListenAndServe()
	if serverErr != nil {
		log.Fatal(serverErr)
	}
}
