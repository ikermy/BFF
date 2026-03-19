package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ikermy/BFF/internal/domain"
)

func TestHTTPClient_ValidateTokenPostsExpectedPayload(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/validate" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}

		var body validateReqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Token != "valid-token" {
			t.Fatalf("expected token valid-token, got %q", body.Token)
		}

		_ = json.NewEncoder(w).Encode(validateRespBody{
			UserID:      "user-1",
			Email:       "john@example.com",
			Permissions: []string{"barcode:read", "barcode:write"},
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	user, err := client.ValidateToken(context.Background(), "valid-token")
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}
	if user.UserID != "user-1" || user.Email != "john@example.com" {
		t.Fatalf("unexpected user: %+v", user)
	}
	if len(user.Permissions) != 2 {
		t.Fatalf("expected 2 permissions, got %+v", user.Permissions)
	}
}

func TestHTTPClient_GetUserInfo(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/users/user-42" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(domain.UserInfo{
			UserID:      "user-42",
			Email:       "u42@example.com",
			Role:        "admin",
			Permissions: []string{"manage:all"},
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	user, err := client.GetUserInfo(context.Background(), "user-42")
	if err != nil {
		t.Fatalf("GetUserInfo returned error: %v", err)
	}
	if user.Role != "admin" || user.UserID != "user-42" {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestHTTPClient_GetUserInfoRejectsEmptyID(t *testing.T) {
	t.Parallel()

	client := NewHTTPClient("http://example.com")
	_, err := client.GetUserInfo(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty userID")
	}
	if !strings.Contains(err.Error(), "userID is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_ValidateTokenReturnsStatusError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	_, err := client.ValidateToken(context.Background(), "bad-token")
	if err == nil {
		t.Fatal("expected error for non-2xx status")
	}
	if !strings.Contains(err.Error(), "/api/v1/validate") || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_GetUserInfoReturnsStatusError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	_, err := client.GetUserInfo(context.Background(), "missing-user")
	if err == nil {
		t.Fatal("expected error for non-2xx status")
	}
	if !strings.Contains(err.Error(), "/api/v1/users/missing-user") || !strings.Contains(err.Error(), "status 404") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMockClient_ValidateTokenAndGetUserInfo(t *testing.T) {
	t.Parallel()

	client := NewMockClient()

	if _, err := client.ValidateToken(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "empty token") {
		t.Fatalf("expected empty token error, got %v", err)
	}
	if _, err := client.ValidateToken(context.Background(), "invalid-token"); err == nil || !strings.Contains(err.Error(), "token rejected") {
		t.Fatalf("expected invalid token error, got %v", err)
	}

	user, err := client.ValidateToken(context.Background(), "valid-token")
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}
	if user.UserID != "mock-user-id" || user.Role != "user" || len(user.Permissions) != 2 {
		t.Fatalf("unexpected validated user: %+v", user)
	}

	if _, err := client.GetUserInfo(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "userID is required") {
		t.Fatalf("expected empty userID error, got %v", err)
	}

	fullUser, err := client.GetUserInfo(context.Background(), "user-42")
	if err != nil {
		t.Fatalf("GetUserInfo returned error: %v", err)
	}
	if fullUser.UserID != "user-42" || fullUser.Email != "user-42@example.com" || fullUser.Role != "user" {
		t.Fatalf("unexpected full user: %+v", fullUser)
	}
}
