package authussy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testUser returns a minimal models.User suitable for token tests.
func testUser() *models.User {
	return &models.User{
		ID:           "user-abc-123",
		Username:     "stallion_king",
		DisplayName:  "Stallion King",
		TokenVersion: 0,
	}
}

// newTestService creates an AuthService with a known secret and expiry.
func newTestService(expiry time.Duration) *AuthService {
	return NewAuthService("test-secret-key-do-not-use-in-prod", expiry)
}

// dummyHandler returns an http.Handler that writes 200 "ok" and stores the
// request context so callers can inspect claims set by middleware.
func dummyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// If claims are present in the context, include the username in the
		// body so tests can verify the middleware injected them.
		if claims, ok := GetUserFromContext(r.Context()); ok {
			fmt.Fprintf(w, "user:%s", claims.Username)
		} else {
			fmt.Fprint(w, "ok")
		}
	})
}

// decodeJSONError parses the {"error":"..."} body returned by the middleware.
func decodeJSONError(t *testing.T, resp *http.Response) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON error body: %v", err)
	}
	return body.Error
}

// ---------------------------------------------------------------------------
// Password Hashing
// ---------------------------------------------------------------------------

func TestHashPassword_RoundTrip(t *testing.T) {
	password := "correct-horse-battery-staple"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if hash == "" {
		t.Fatal("HashPassword returned empty hash")
	}
	if hash == password {
		t.Fatal("HashPassword returned the plaintext password")
	}

	if !CheckPassword(hash, password) {
		t.Error("CheckPassword should return true for the correct password")
	}
}

func TestCheckPassword_WrongPassword(t *testing.T) {
	hash, err := HashPassword("real-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if CheckPassword(hash, "wrong-password") {
		t.Error("CheckPassword should return false for an incorrect password")
	}
}

func TestHashPassword_EmptyString(t *testing.T) {
	// bcrypt can hash an empty string — verify we don't panic or error.
	hash, err := HashPassword("")
	if err != nil {
		t.Fatalf("HashPassword with empty string returned error: %v", err)
	}
	if hash == "" {
		t.Fatal("HashPassword returned empty hash for empty password")
	}
	if !CheckPassword(hash, "") {
		t.Error("CheckPassword should match for empty password")
	}
	if CheckPassword(hash, "not-empty") {
		t.Error("CheckPassword should reject non-empty password against empty-string hash")
	}
}

func TestHashPassword_DifferentHashesPerCall(t *testing.T) {
	// bcrypt includes a random salt — two hashes of the same password must differ.
	h1, _ := HashPassword("same-password")
	h2, _ := HashPassword("same-password")
	if h1 == h2 {
		t.Error("two bcrypt hashes of the same password should differ (random salt)")
	}
}

// ---------------------------------------------------------------------------
// JWT Token Generation & Validation
// ---------------------------------------------------------------------------

func TestGenerateToken_ValidateToken_RoundTrip(t *testing.T) {
	svc := newTestService(1 * time.Hour)
	user := testUser()

	token, err := svc.GenerateToken(user)
	if err != nil {
		t.Fatalf("GenerateToken error: %v", err)
	}
	if token == "" {
		t.Fatal("GenerateToken returned empty string")
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken error: %v", err)
	}

	if claims.UserID != user.ID {
		t.Errorf("UserID = %q, want %q", claims.UserID, user.ID)
	}
	if claims.Username != user.Username {
		t.Errorf("Username = %q, want %q", claims.Username, user.Username)
	}
	if claims.DisplayName != user.DisplayName {
		t.Errorf("DisplayName = %q, want %q", claims.DisplayName, user.DisplayName)
	}
	if claims.TokenVersion != user.TokenVersion {
		t.Errorf("TokenVersion = %d, want %d", claims.TokenVersion, user.TokenVersion)
	}
	if claims.Issuer != "stallionussy" {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, "stallionussy")
	}
	if claims.Subject != user.ID {
		t.Errorf("Subject = %q, want %q", claims.Subject, user.ID)
	}
}

func TestGenerateToken_ClaimsContainTokenVersion(t *testing.T) {
	svc := newTestService(1 * time.Hour)
	user := testUser()
	user.TokenVersion = 42

	token, err := svc.GenerateToken(user)
	if err != nil {
		t.Fatalf("GenerateToken error: %v", err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken error: %v", err)
	}
	if claims.TokenVersion != 42 {
		t.Errorf("TokenVersion = %d, want 42", claims.TokenVersion)
	}
}

func TestValidateToken_RejectsExpired(t *testing.T) {
	svc := newTestService(1 * time.Hour)

	// Manually craft an already-expired token (NewAuthService clamps
	// negative durations to 1h, so we build the claims ourselves).
	past := time.Now().Add(-2 * time.Hour)
	claims := Claims{
		UserID:   "user-abc-123",
		Username: "stallion_king",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(past),
			IssuedAt:  jwt.NewNumericDate(past.Add(-1 * time.Hour)),
			NotBefore: jwt.NewNumericDate(past.Add(-1 * time.Hour)),
			Issuer:    "stallionussy",
			Subject:   "user-abc-123",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("test-secret-key-do-not-use-in-prod"))
	if err != nil {
		t.Fatalf("failed to sign expired token: %v", err)
	}

	_, err = svc.ValidateToken(signed)
	if err == nil {
		t.Fatal("ValidateToken should reject an expired token")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("expected error to mention 'token', got: %v", err)
	}
}

func TestValidateToken_RejectsWrongSecret(t *testing.T) {
	svc1 := NewAuthService("secret-one", 1*time.Hour)
	svc2 := NewAuthService("secret-two", 1*time.Hour)

	token, err := svc1.GenerateToken(testUser())
	if err != nil {
		t.Fatalf("GenerateToken error: %v", err)
	}

	_, err = svc2.ValidateToken(token)
	if err == nil {
		t.Fatal("ValidateToken should reject a token signed with a different secret")
	}
}

func TestValidateToken_RejectsMalformed(t *testing.T) {
	svc := newTestService(1 * time.Hour)

	for _, bad := range []string{
		"",
		"not-a-jwt",
		"eyJhbGciOiJIUzI1NiJ9.garbage.garbage",
		"a.b.c",
	} {
		_, err := svc.ValidateToken(bad)
		if err == nil {
			t.Errorf("ValidateToken(%q) should have returned an error", bad)
		}
	}
}

func TestValidateToken_RejectsNonHMAC(t *testing.T) {
	// Craft a token with "none" algorithm to ensure we reject it.
	claims := Claims{
		UserID:   "hacker",
		Username: "h4cker",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("failed to craft none-signed token: %v", err)
	}

	svc := newTestService(1 * time.Hour)
	_, err = svc.ValidateToken(signed)
	if err == nil {
		t.Fatal("ValidateToken should reject a token signed with 'none' algorithm")
	}
}

func TestNewAuthService_DefaultExpiry(t *testing.T) {
	svc := NewAuthService("secret", 0)
	if svc.expiration != 1*time.Hour {
		t.Errorf("default expiration = %v, want 1h", svc.expiration)
	}
	svc2 := NewAuthService("secret", -5*time.Minute)
	if svc2.expiration != 1*time.Hour {
		t.Errorf("negative expiration should default to 1h, got %v", svc2.expiration)
	}
}

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

func TestGetUserFromContext_Present(t *testing.T) {
	claims := &Claims{UserID: "u1", Username: "alice"}
	ctx := context.WithValue(context.Background(), UserContextKey, claims)

	got, ok := GetUserFromContext(ctx)
	if !ok {
		t.Fatal("expected claims in context")
	}
	if got.UserID != "u1" {
		t.Errorf("UserID = %q, want %q", got.UserID, "u1")
	}
}

func TestGetUserFromContext_Missing(t *testing.T) {
	_, ok := GetUserFromContext(context.Background())
	if ok {
		t.Fatal("expected no claims in empty context")
	}
}

// ---------------------------------------------------------------------------
// Auth Middleware
// ---------------------------------------------------------------------------

func TestMiddleware_SkipPaths(t *testing.T) {
	svc := newTestService(1 * time.Hour)
	handler := svc.AuthMiddleware(dummyHandler())

	// All paths in the skip list should pass through without any auth header.
	for path := range skipAuthPaths {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("skip path %q: got status %d, want 200", path, rec.Code)
		}
	}
}

func TestMiddleware_NonAPIPath_SkipsAuth(t *testing.T) {
	svc := newTestService(1 * time.Hour)
	handler := svc.AuthMiddleware(dummyHandler())

	// A path that doesn't start with /api/ should be passed through.
	req := httptest.NewRequest("GET", "/static/app.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("non-API path: got status %d, want 200", rec.Code)
	}
}

func TestMiddleware_ValidToken_PassesThrough(t *testing.T) {
	svc := newTestService(1 * time.Hour)
	user := testUser()
	token, _ := svc.GenerateToken(user)

	handler := svc.AuthMiddleware(dummyHandler())
	req := httptest.NewRequest("GET", "/api/stables", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("valid token: got status %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "user:stallion_king") {
		t.Errorf("expected body to contain username from claims, got %q", body)
	}
}

func TestMiddleware_InvalidToken_Returns401(t *testing.T) {
	svc := newTestService(1 * time.Hour)
	handler := svc.AuthMiddleware(dummyHandler())

	req := httptest.NewRequest("GET", "/api/stables", nil)
	req.Header.Set("Authorization", "Bearer garbage-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("invalid token: got status %d, want 401", rec.Code)
	}
}

func TestMiddleware_NoAuthHeader_Returns401(t *testing.T) {
	svc := newTestService(1 * time.Hour)
	handler := svc.AuthMiddleware(dummyHandler())

	req := httptest.NewRequest("GET", "/api/stables", nil)
	// No Authorization header.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no auth header: got status %d, want 401", rec.Code)
	}
	errMsg := decodeJSONError(t, rec.Result())
	if !strings.Contains(errMsg, "missing") {
		t.Errorf("expected error about missing header, got %q", errMsg)
	}
}

func TestMiddleware_BadAuthFormat_Returns401(t *testing.T) {
	svc := newTestService(1 * time.Hour)
	handler := svc.AuthMiddleware(dummyHandler())

	// Send a non-Bearer auth header.
	req := httptest.NewRequest("GET", "/api/stables", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("bad auth format: got status %d, want 401", rec.Code)
	}
}

func TestMiddleware_ExpiredToken_Returns401(t *testing.T) {
	svc := newTestService(1 * time.Hour)

	// Craft an already-expired token manually (same secret as svc).
	past := time.Now().Add(-2 * time.Hour)
	claims := Claims{
		UserID:   "user-abc-123",
		Username: "stallion_king",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(past),
			IssuedAt:  jwt.NewNumericDate(past.Add(-1 * time.Hour)),
			NotBefore: jwt.NewNumericDate(past.Add(-1 * time.Hour)),
			Issuer:    "stallionussy",
			Subject:   "user-abc-123",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := token.SignedString([]byte("test-secret-key-do-not-use-in-prod"))

	handler := svc.AuthMiddleware(dummyHandler())
	req := httptest.NewRequest("GET", "/api/stables", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expired token: got status %d, want 401", rec.Code)
	}
}

func TestMiddleware_TokenVersionMismatch_Returns401(t *testing.T) {
	svc := newTestService(1 * time.Hour)

	// Wire up a GetTokenVersion callback that returns version 1 — the
	// token was generated with version 0, so it should be rejected.
	svc.GetTokenVersion = func(ctx context.Context, userID string) (int, error) {
		return 1, nil // DB says version 1, but token has version 0
	}

	user := testUser() // TokenVersion = 0
	token, _ := svc.GenerateToken(user)

	handler := svc.AuthMiddleware(dummyHandler())
	req := httptest.NewRequest("GET", "/api/stables", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("version mismatch: got status %d, want 401", rec.Code)
	}
	errMsg := decodeJSONError(t, rec.Result())
	if !strings.Contains(errMsg, "revoked") {
		t.Errorf("expected error about revoked token, got %q", errMsg)
	}
}

func TestMiddleware_TokenVersionMatch_Passes(t *testing.T) {
	svc := newTestService(1 * time.Hour)

	svc.GetTokenVersion = func(ctx context.Context, userID string) (int, error) {
		return 0, nil // Matches user.TokenVersion
	}

	user := testUser() // TokenVersion = 0
	token, _ := svc.GenerateToken(user)

	handler := svc.AuthMiddleware(dummyHandler())
	req := httptest.NewRequest("GET", "/api/stables", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("matching version: got status %d, want 200", rec.Code)
	}
}

func TestMiddleware_GetTokenVersionError_Returns401(t *testing.T) {
	svc := newTestService(1 * time.Hour)

	svc.GetTokenVersion = func(ctx context.Context, userID string) (int, error) {
		return 0, fmt.Errorf("database is on fire")
	}

	user := testUser()
	token, _ := svc.GenerateToken(user)

	handler := svc.AuthMiddleware(dummyHandler())
	req := httptest.NewRequest("GET", "/api/stables", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("DB error: got status %d, want 401", rec.Code)
	}
}

func TestMiddleware_NilGetTokenVersion_SkipsCheck(t *testing.T) {
	// When GetTokenVersion is nil, the middleware should not check the
	// version — useful for test/in-memory mode.
	svc := newTestService(1 * time.Hour)
	svc.GetTokenVersion = nil

	user := testUser()
	user.TokenVersion = 99 // Any version should be accepted
	token, _ := svc.GenerateToken(user)

	handler := svc.AuthMiddleware(dummyHandler())
	req := httptest.NewRequest("GET", "/api/stables", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("nil GetTokenVersion should skip check, got status %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Optional-auth paths (GET /api/races/* and POST /api/races/quick)
// ---------------------------------------------------------------------------

func TestMiddleware_OptionalAuth_GET_Race_NoToken(t *testing.T) {
	svc := newTestService(1 * time.Hour)
	handler := svc.AuthMiddleware(dummyHandler())

	req := httptest.NewRequest("GET", "/api/races/some-race-id", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("optional auth GET race without token: got %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "user:") {
		t.Errorf("expected no user in context, got %q", body)
	}
}

func TestMiddleware_OptionalAuth_GET_Race_WithToken(t *testing.T) {
	svc := newTestService(1 * time.Hour)
	user := testUser()
	token, _ := svc.GenerateToken(user)
	handler := svc.AuthMiddleware(dummyHandler())

	req := httptest.NewRequest("GET", "/api/races/some-race-id", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("optional auth GET race with token: got %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "user:stallion_king") {
		t.Errorf("expected user in context, got %q", body)
	}
}

func TestMiddleware_OptionalAuth_POST_QuickRace_NoToken(t *testing.T) {
	svc := newTestService(1 * time.Hour)
	handler := svc.AuthMiddleware(dummyHandler())

	req := httptest.NewRequest("POST", "/api/races/quick", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("optional auth POST /api/races/quick without token: got %d, want 200", rec.Code)
	}
}

func TestMiddleware_OptionalAuth_InvalidToken_StillPasses(t *testing.T) {
	svc := newTestService(1 * time.Hour)
	handler := svc.AuthMiddleware(dummyHandler())

	// An invalid token on an optional-auth path should still pass through,
	// just without claims in the context.
	req := httptest.NewRequest("GET", "/api/races/some-id", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("optional auth with bad token: got %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "user:") {
		t.Errorf("bad token should not inject claims, got %q", body)
	}
}

// ---------------------------------------------------------------------------
// Username Validation
// ---------------------------------------------------------------------------

func TestUsernameRegex_ValidUsernames(t *testing.T) {
	valid := []string{
		"abc",                              // minimum length (3)
		"ABC",                              // uppercase
		"a1b2c3",                           // mixed alphanumeric
		"under_score",                      // underscores allowed
		"_a_",                              // starts/ends with underscore
		"12345678901234567890123456789012", // exactly 32 chars
		"stallion_king",
		"user123",
	}
	for _, u := range valid {
		if !usernameRegex.MatchString(u) {
			t.Errorf("expected username %q to be valid", u)
		}
	}
}

func TestUsernameRegex_InvalidUsernames(t *testing.T) {
	invalid := []string{
		"",                                  // empty
		"ab",                                // too short (2 chars)
		"a",                                 // too short (1 char)
		"123456789012345678901234567890123", // 33 chars — too long
		"hello world",                       // spaces
		"hello-world",                       // hyphens
		"user@name",                         // @ symbol
		"user.name",                         // period
		"special!chars",                     // exclamation
		"no spaces here",                    // multiple spaces
		"tab\there",                         // tab character
	}
	for _, u := range invalid {
		if usernameRegex.MatchString(u) {
			t.Errorf("expected username %q to be invalid", u)
		}
	}
}

// ---------------------------------------------------------------------------
// JSON helper tests (writeJSON / writeJSONError)
// ---------------------------------------------------------------------------

func TestWriteJSON_SetsContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]string{"hello": "world"})

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestWriteJSONError_Format(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSONError(rec, http.StatusBadRequest, "something broke")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}

	var body jsonError
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body.Error != "something broke" {
		t.Errorf("error = %q, want %q", body.Error, "something broke")
	}
}
