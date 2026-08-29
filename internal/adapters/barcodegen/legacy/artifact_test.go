package legacy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikermy/BFF/internal/domain"
)

// TestLocalArtifactStore_SavesAndReturnsPublicURL — скачивает src, пишет в dir,
// возвращает publicBaseURL/{key}.png.
func TestLocalArtifactStore_SavesAndReturnsPublicURL(t *testing.T) {
	// Источник PNG.
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("PNG-DATA"))
	}))
	defer src.Close()

	dir := t.TempDir()
	store := NewLocalArtifactStore(dir, "https://cdn.example.com/artifacts")

	url, err := store.Save(context.Background(), src.URL, "build-1:0", "png")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.HasPrefix(url, "https://cdn.example.com/artifacts/") || !strings.HasSuffix(url, ".png") {
		t.Errorf("unexpected public URL: %q", url)
	}

	// Файл реально создан в dir и содержит данные.
	name := strings.TrimPrefix(url, "https://cdn.example.com/artifacts/")
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("artifact file missing: %v", err)
	}
	if string(data) != "PNG-DATA" {
		t.Errorf("artifact content = %q, want PNG-DATA", data)
	}
}

// TestLocalArtifactStore_NoPublicBaseReturnsPath — без publicBaseURL возвращает file-путь.
func TestLocalArtifactStore_NoPublicBaseReturnsPath(t *testing.T) {
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	defer src.Close()

	store := NewLocalArtifactStore(t.TempDir(), "")
	url, err := store.Save(context.Background(), src.URL, "k", "png")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.HasSuffix(url, ".png") {
		t.Errorf("expected file path ending .png, got %q", url)
	}
}

// TestLegacyClient_GeneratePDF417_RelocatesArtifact — перекладка подменяет barcodeUrl.
func TestLegacyClient_GeneratePDF417_RelocatesArtifact(t *testing.T) {
	var bg *httptest.Server
	// BarcodeGen отдаёт PNG URL на свой диск.
	bg = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/barcodes/pdf417":
			_ = json.NewEncoder(w).Encode(map[string]any{"url": bg.URL + "/uploads/x.png", "data": "ANSI"})
		default: // BarcodeGen отдаёт сам PNG.
			_, _ = w.Write([]byte("PNG"))
		}
	}))
	defer bg.Close()

	dir := t.TempDir()
	c := NewLegacyClient(bg.URL, testSecret, "", 5*time.Second).
		WithArtifactStore(NewLocalArtifactStore(dir, "https://cdn.example.com/artifacts"))

	resp, err := c.GeneratePDF417(context.Background(), domain.GeneratePDF417Request{
		Revision: "US_CA_08292017",
		BuildID:  "b1",
		Fields:   map[string]any{"firstName": "John"},
	})
	if err != nil {
		t.Fatalf("GeneratePDF417: %v", err)
	}
	// barcodeUrl должен указывать на CDN-артефакт, а не на диск BarcodeGen.
	if !strings.HasPrefix(resp.BarcodeURL, "https://cdn.example.com/artifacts/") {
		t.Errorf("expected relocated URL, got %q", resp.BarcodeURL)
	}
}
