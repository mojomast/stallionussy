package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func getStatus(t *testing.T, s *Server) map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("/api/status: %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	return body
}

// TestStatusEndpointReportsMode: /api/status must say "offline" on SQLite and
// "online" otherwise (additive API — mode/storage are new fields).
func TestStatusEndpointReportsMode(t *testing.T) {
	t.Setenv("JWT_SECRET", "status-test-secret")

	db := openTestServerDB(t, filepath.Join(t.TempDir(), "status.db"))
	defer db.Close()
	offlineServer := NewServer(db)
	body := getStatus(t, offlineServer)
	if body["mode"] != "offline" || body["storage"] != "sqlite" {
		t.Errorf("sqlite server status = %v", body)
	}
	if body["status"] != "ok" || body["app"] != "stallionussy" {
		t.Errorf("status body missing basics: %v", body)
	}

	memServer := NewServer(nil)
	body = getStatus(t, memServer)
	if body["mode"] != "online" || body["storage"] != "memory" {
		t.Errorf("in-memory server status = %v", body)
	}
}

// TestOfflineZeroConfigSecret: offline mode must boot with NO JWT_SECRET,
// generate a local secret, persist it, and reuse it across restarts so
// tokens issued before a restart still validate after it.
func TestOfflineZeroConfigSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "") // explicitly unset
	dbPath := filepath.Join(t.TempDir(), "zeroconf.db")

	db1 := openTestServerDB(t, dbPath)
	s1 := NewServer(db1)
	token, user := registerTestUser(t, s1, "zeroconfussy")
	if claims, err := s1.auth.ValidateToken(token); err != nil || claims.UserID != user.ID {
		t.Fatalf("token invalid on issuing server: %v", err)
	}
	db1.Close()

	// Restart: the persisted secret must validate the old token.
	db2 := openTestServerDB(t, dbPath)
	defer db2.Close()
	s2 := NewServer(db2)
	if claims, err := s2.auth.ValidateToken(token); err != nil || claims.UserID != user.ID {
		t.Fatalf("token no longer valid after restart (secret not persisted?): %v", err)
	}
	if err := s2.auth.ValidateSession(context.Background(), token); err != nil {
		t.Fatalf("session invalid after zero-config restart: %v", err)
	}
}
