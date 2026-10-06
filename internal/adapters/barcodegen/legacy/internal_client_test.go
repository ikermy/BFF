package legacy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ikermy/BFF/internal/adapters/barcodegen"
	"github.com/ikermy/BFF/internal/domain"
)

func TestInternalClientRandom(t *testing.T) {
	var gotToken, gotAuth, gotPath string
	var gotBody internalFieldSetReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Service-Token")
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"values":{"DBB":"01011990"}}`))
	}))
	defer srv.Close()

	c := NewInternalClient(srv.URL, "secret", "svc-token", "", 5*time.Second)
	val, err := c.Random(context.Background(), "", "DBB", map[string]any{"DAJ": "AR"})
	if err != nil {
		t.Fatalf("Random error: %v", err)
	}
	if gotToken != "svc-token" {
		t.Fatalf("token header = %q, want svc-token", gotToken)
	}
	if gotAuth != "Bearer svc-token" {
		t.Fatalf("authorization = %q, want Bearer svc-token", gotAuth)
	}
	if gotPath != "/api/internal/v1/barcodes/random" {
		t.Fatalf("path = %q", gotPath)
	}
	if len(gotBody.Outputs) != 1 || gotBody.Outputs[0] != "DBB" {
		t.Fatalf("outputs = %v", gotBody.Outputs)
	}
	if val != "01011990" {
		t.Fatalf("value = %v", val)
	}
}

func TestInternalClientCalculate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/internal/v1/barcodes/calculate" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"values":{"DBA":"07192028"}}`))
	}))
	defer srv.Close()

	c := NewInternalClient(srv.URL, "secret", "svc-token", "", 5*time.Second)
	val, err := c.Calculate(context.Background(), "", "DBA", map[string]any{"DBD": "04162023"})
	if err != nil {
		t.Fatalf("Calculate error: %v", err)
	}
	if val != "07192028" {
		t.Fatalf("value = %v", val)
	}
}

func TestInternalClientGeneratePDF417(t *testing.T) {
	var got internalPDF417Req
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/internal/v1/barcodes/pdf417" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"id":"abc","url":"https://cdn.local/abc.png"}`))
	}))
	defer srv.Close()

	c := NewInternalClient(srv.URL, "secret", "svc-token", "", 5*time.Second)
	ctx := barcodegen.WithUserID(context.Background(), "user-1")

	resp, err := c.GeneratePDF417(ctx, domain.GeneratePDF417Request{
		BuildID:        "build-1",
		IdempotencyKey: "idem-1",
		Fields: map[string]any{
			"DAJ": "AR", "DDB": "03012018", "QQQ": "DL",
		},
	})
	if err != nil {
		t.Fatalf("GeneratePDF417 error: %v", err)
	}
	if !resp.Success || resp.BarcodeURL != "https://cdn.local/abc.png" {
		t.Fatalf("resp = %+v", resp)
	}
	if got.OwnerID != "user-1" {
		t.Fatalf("ownerId = %q, want user-1", got.OwnerID)
	}
	if got.RenderKey != "idem-1" {
		t.Fatalf("renderKey = %q, want idem-1", got.RenderKey)
	}
	if got.Values["DAJ"] != "AR" || got.Values["QQQ"] != "DL" {
		t.Fatalf("values = %v", got.Values)
	}
}

// ── Code128 идёт через internal route с X-Service-Token ──

func TestInternalClientCode128(t *testing.T) {
	var gotPath, gotToken string
	var got internalCode128Req
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Service-Token")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"id":"c1","url":"https://cdn.local/c1.png"}`))
	}))
	defer srv.Close()

	c := NewInternalClient(srv.URL, "secret", "svc-token", "", 5*time.Second)
	ctx := barcodegen.WithUserID(context.Background(), "user-1")
	resp, err := c.GenerateCode128(ctx, domain.GenerateCode128Request{Data: "INV-1", BuildID: "b1"})
	if err != nil {
		t.Fatalf("GenerateCode128 error: %v", err)
	}
	if !resp.Success || resp.BarcodeURL != "https://cdn.local/c1.png" {
		t.Fatalf("resp = %+v", resp)
	}
	if gotPath != "/api/internal/v1/barcodes/code128" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotToken != "svc-token" {
		t.Fatalf("token = %q", gotToken)
	}
	if got.OwnerID != "user-1" || got.Value != "INV-1" {
		t.Fatalf("body = %+v", got)
	}
}
