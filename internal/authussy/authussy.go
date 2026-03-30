// Package authussy provides JWT-based authentication for StallionUSSY.
//
// It includes password hashing (bcrypt), JWT token generation/validation,
// HTTP middleware for protecting API routes, and auth handlers for
// login/register/me endpoints.
package authussy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

// ---------------------------------------------------------------------------
// Password Hashing
// ---------------------------------------------------------------------------

const bcryptCost = 10

// HashPassword hashes a plaintext password using bcrypt with cost 10.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("authussy: hash password: %w", err)
	}
	return string(bytes), nil
}

// CheckPassword returns true if the plaintext password matches the bcrypt hash.
func CheckPassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ---------------------------------------------------------------------------
// JWT Claims
// ---------------------------------------------------------------------------

// Claims represents the JWT payload for authenticated users.
type Claims struct {
	UserID       string `json:"user_id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	TokenVersion int    `json:"token_version"`
	jwt.RegisteredClaims
}

// ---------------------------------------------------------------------------
// AuthService — token generation and validation
// ---------------------------------------------------------------------------

// AuthService handles JWT token operations.
type AuthService struct {
	secret     []byte
	expiration time.Duration
	// GetTokenVersion is an optional callback that retrieves the current
	// token_version from the DB for a given user ID. When set, the auth
	// middleware will reject tokens whose version doesn't match.
	GetTokenVersion func(ctx context.Context, userID string) (int, error)
}

// NewAuthService creates a new AuthService. If expiration is 0, it defaults
// to 1 hour.
func NewAuthService(secret string, expiration time.Duration) *AuthService {
	if expiration <= 0 {
		expiration = 1 * time.Hour
	}
	return &AuthService{
		secret:     []byte(secret),
		expiration: expiration,
	}
}

// GenerateToken creates a signed JWT for the given user.
func (a *AuthService) GenerateToken(user *models.User) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:       user.ID,
		Username:     user.Username,
		DisplayName:  user.DisplayName,
		TokenVersion: user.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(a.expiration)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "stallionussy",
			Subject:   user.ID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(a.secret)
	if err != nil {
		return "", fmt.Errorf("authussy: sign token: %w", err)
	}
	return signed, nil
}

// ValidateToken parses and validates a JWT string, returning the claims if
// the token is valid.
func (a *AuthService) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Ensure the signing method is HMAC.
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("authussy: unexpected signing method: %v", token.Header["alg"])
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("authussy: validate token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("authussy: invalid token claims")
	}
	return claims, nil
}

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

type contextKey string

// UserContextKey is the context key used to store Claims in the request context.
const UserContextKey contextKey = "user"

// GetUserFromContext extracts the authenticated user's claims from the context.
// Returns nil, false if no claims are present.
func GetUserFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(UserContextKey).(*Claims)
	return claims, ok
}

// ---------------------------------------------------------------------------
// HTTP Middleware
// ---------------------------------------------------------------------------

// skipAuthPaths are exact-match paths that bypass authentication.
var skipAuthPaths = map[string]bool{
	"/api/auth/login":    true,
	"/api/auth/register": true,
	"/api/capabilities":  true,
	"/ws":                true,
	"/":                  true,
}

// AuthMiddleware returns an HTTP middleware that validates JWT tokens on
// incoming requests. Paths in the skip list and paths not starting with
// "/api/" are passed through without authentication.
func (a *AuthService) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Skip auth for certain paths.
		if skipAuthPaths[path] {
			next.ServeHTTP(w, r)
			return
		}

		// Skip auth for shareable/read-only race endpoints (GET) and the
		// quick-race endpoint (POST) which supports both guest and
		// authenticated callers.  When a valid Bearer token is present we
		// still decode it and store claims in context so that handlers can
		// differentiate authenticated users from guests.
		optionalAuth := false
		if strings.HasPrefix(path, "/api/races/") && r.Method == "GET" {
			optionalAuth = true
		}
		if path == "/api/races/quick" && r.Method == "POST" {
			optionalAuth = true
		}
		if optionalAuth {
			if authHeader := r.Header.Get("Authorization"); authHeader != "" {
				if parts := strings.SplitN(authHeader, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
					if claims, err := a.ValidateToken(parts[1]); err == nil {
						ctx := context.WithValue(r.Context(), UserContextKey, claims)
						r = r.WithContext(ctx)
					}
				}
			}
			next.ServeHTTP(w, r)
			return
		}

		// Skip auth for non-API paths.
		if !strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		// Extract the Bearer token from the Authorization header.
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeJSONError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeJSONError(w, http.StatusUnauthorized, "invalid authorization header format")
			return
		}

		tokenString := parts[1]
		claims, err := a.ValidateToken(tokenString)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		// Check token_version against the DB to reject revoked tokens
		// (e.g. after a password change).
		if a.GetTokenVersion != nil {
			dbVersion, verr := a.GetTokenVersion(r.Context(), claims.UserID)
			if verr != nil {
				writeJSONError(w, http.StatusUnauthorized, "failed to verify token version")
				return
			}
			if claims.TokenVersion != dbVersion {
				writeJSONError(w, http.StatusUnauthorized, "token has been revoked")
				return
			}
		}

		// Store claims in request context and continue.
		ctx := context.WithValue(r.Context(), UserContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ---------------------------------------------------------------------------
// Auth Handlers
// ---------------------------------------------------------------------------

// CreateStableFunc is a function that creates a stable for a newly registered
// user. It receives the stable name and owner ID and returns the created
// stable. This is injected to avoid circular dependencies with the game logic.
type CreateStableFunc func(name, ownerID string) *models.Stable

// AuthHandler provides HTTP handlers for authentication endpoints.
type AuthHandler struct {
	auth         *AuthService
	users        repository.UserRepository
	createStable CreateStableFunc
}

// NewAuthHandler creates a new AuthHandler. The createStableFn is optional and
// can be nil — if nil, registration will still succeed but no stable will be
// created automatically.
func NewAuthHandler(auth *AuthService, users repository.UserRepository, createStableFn CreateStableFunc) *AuthHandler {
	return &AuthHandler{
		auth:         auth,
		users:        users,
		createStable: createStableFn,
	}
}

// usernameRegex validates usernames: 3-32 chars, alphanumeric + underscore.
var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

// registerRequest is the expected JSON body for registration.
type registerRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// loginRequest is the expected JSON body for login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// authResponse is the JSON response for successful login/register.
type authResponse struct {
	Token string       `json:"token"`
	User  *models.User `json:"user"`
}

// HandleRegister handles POST /api/auth/register.
//
// It validates the input, hashes the password, creates the user,
// optionally creates a stable, and returns a JWT + user object.
func (h *AuthHandler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Limit request body size to 1MB to prevent abuse.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Validate username.
	if !usernameRegex.MatchString(req.Username) {
		writeJSONError(w, http.StatusBadRequest, "username must be 3-32 characters, alphanumeric and underscores only")
		return
	}

	// Validate password length.
	if len(req.Password) < 8 {
		writeJSONError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	// Default display name to username if not provided.
	displayName := req.DisplayName
	if displayName == "" {
		displayName = req.Username
	}

	// Check if username is already taken.
	existing, err := h.users.GetUserByUsername(r.Context(), req.Username)
	if err != nil && existing == nil {
		// Database error during lookup — log but continue cautiously.
		log.Printf("authussy: user lookup failed for %q: %v", req.Username, err)
	}
	if existing != nil {
		writeJSONError(w, http.StatusConflict, "username already taken")
		return
	}

	// Hash the password.
	hash, err := HashPassword(req.Password)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	// Create the user.
	now := time.Now()
	user := &models.User{
		ID:           uuid.New().String(),
		Username:     req.Username,
		PasswordHash: hash,
		DisplayName:  displayName,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := h.users.CreateUser(r.Context(), user); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	// Create a stable for the new user if the function is provided.
	if h.createStable != nil {
		h.createStable(displayName+"'s Stable", user.ID)
	}

	// Generate a JWT for auto-login.
	token, err := h.auth.GenerateToken(user)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{
		Token: token,
		User:  user,
	})
}

// HandleLogin handles POST /api/auth/login.
//
// It validates credentials and returns a JWT + user object on success.
func (h *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Limit request body size to 1MB to prevent abuse.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Look up user by username.
	user, err := h.users.GetUserByUsername(r.Context(), req.Username)
	if err != nil || user == nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	// Check password.
	if !CheckPassword(user.PasswordHash, req.Password) {
		writeJSONError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	// Generate token.
	token, err := h.auth.GenerateToken(user)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{
		Token: token,
		User:  user,
	})
}

// HandleMe handles GET /api/auth/me.
//
// It returns the full user object for the currently authenticated user.
func (h *AuthHandler) HandleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	claims, ok := GetUserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	user, err := h.users.GetUserByID(r.Context(), claims.UserID)
	if err != nil || user == nil {
		writeJSONError(w, http.StatusNotFound, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, user)
}

// HandleRefresh handles POST /api/auth/refresh.
//
// It requires a valid (non-expired) JWT, verifies the token_version against
// the DB, and issues a new token with a fresh expiry. This lets clients stay
// logged in without needing long-lived tokens.
func (h *AuthHandler) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	claims, ok := GetUserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	// Fetch the user from DB to verify token_version and get current state.
	user, err := h.users.GetUserByID(r.Context(), claims.UserID)
	if err != nil || user == nil {
		writeJSONError(w, http.StatusUnauthorized, "user not found")
		return
	}

	// Verify the token's version matches the DB — reject if password was changed.
	if claims.TokenVersion != user.TokenVersion {
		writeJSONError(w, http.StatusUnauthorized, "token has been revoked (password was changed)")
		return
	}

	// Issue a fresh token with updated user state.
	token, err := h.auth.GenerateToken(user)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{
		Token: token,
		User:  user,
	})
}

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

// jsonError is the standard error response format.
type jsonError struct {
	Error string `json:"error"`
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data) //nolint:errcheck
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, jsonError{Error: message})
}
