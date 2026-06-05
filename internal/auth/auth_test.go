package auth

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// PASSWORD TESTS
func TestHashPassword(t *testing.T) {
	password := "super-secret-not-at-all-confusing-password"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal("failed to hash password")
	}
	if hash == "" {
		t.Fatal("hash is empty")
	}
	if hash == password {
		t.Fatal("hash should not be rqual to raw pw")
	}
}

func TestCheckPasswordHash_InvalidPassword(t *testing.T) {
	password := "super-secret-not-at-all-confusing-password"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal("failed to hash password")
	}

	match, err := CheckPasswordHash(password, hash)
	if err != nil {
		t.Fatal("failed to check hash against password")
	}

	if !match {
		t.Fatal("Expected password to match hash")
	}
}

func TestCheckPasswordHash_InvalidHash(t *testing.T) {
	password := "some-password"
	invalidHash := "not-a-real-hash"

	match, err := CheckPasswordHash(password, invalidHash)

	if err == nil {
		t.Fatal("expected error for invalid hash")
	}

	if match {
		t.Fatal("expected match to be false")
	}
}

func TestHashPassword_UniqueHashes(t *testing.T) {
	password := "same-password"

	hash1, err := HashPassword(password)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	hash2, err := HashPassword(password)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	// Argon2id should generate different hashes because of unique salts
	if hash1 == hash2 {
		t.Fatal("expected hashes to be different for same password")
	}
}

// JWT TESTS
func TestValidateJWT(t *testing.T) {
	userID := uuid.New()
	validToken, _ := MakeJWT(userID, "secret")

	tests := []struct {
		name        string
		tokenString string
		tokenSecret string
		wantUserID  uuid.UUID
		wantErr     bool
	}{
		{
			name:        "Valid token",
			tokenString: validToken,
			tokenSecret: "secret",
			wantUserID:  userID,
			wantErr:     false,
		},
		{
			name:        "Wrong secret",
			tokenString: validToken,
			tokenSecret: "wrong_secret",
			wantUserID:  uuid.Nil,
			wantErr:     true,
		},
		{
			name:        "invalid token",
			tokenString: "hehe xd",
			tokenSecret: "secret",
			wantUserID:  uuid.Nil,
			wantErr:     true,
		},
		// add expired and malformed cases...
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, err := ValidateJWT(tt.tokenString, tt.tokenSecret)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateJWT() error = %v, wantErr %v", err, tt.wantErr)
			}
			if gotID != tt.wantUserID {
				t.Errorf("ValidateJWT() gotID = %v, want %v", gotID, tt.wantUserID)
			}
		})
	}
}

func TestGetBearerToken_Success(t *testing.T) {
	headers := http.Header{}
	headers.Set("Bearer", "my-test-token")

	token, err := GetBearerToken(headers)
	if err != nil {
		t.Logf("expected no error, got %v", err)
	}

	if token != "my-test-token" {
		t.Logf("expected token %q, got %q", "my-test-token", token)
	}
}

func TestGetBearerToken_MissingHeader(t *testing.T) {
	headers := http.Header{}

	token, err := GetBearerToken(headers)

	if err == nil {
		t.Log("expected an error, got nil")
	}

	if token != "" {
		t.Logf("expected empty token, got %q", token)
	}

	expectedErr := "auth header does not exist"
	if err.Error() != expectedErr {
		t.Logf("expected error %q, got %q", expectedErr, err.Error())
	}
}

func TestGetBearerToken_EmptyHeader(t *testing.T) {
	headers := http.Header{}
	headers.Set("Bearer", "")

	token, err := GetBearerToken(headers)

	if err == nil {
		t.Log("expected an error, got nil")
	}

	if token != "" {
		t.Logf("expected empty token, got %q", token)
	}
}
