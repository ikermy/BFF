package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ArtifactStore — перекладка сгенерированного PNG в стабильное хранилище (D2, этап 2).
//
// BarcodeGen пишет PNG на локальный диск uploads/barcodes и отдаёт URL через BARCODE_URL.
// URL перестаёт работать при пересоздании контейнера. Перекладка переносит файл в
// персистентное место (общий volume / S3 / CDN) и подменяет barcodeUrl в ответе и событии,
// чтобы ссылка жила после рестарта BarcodeGen.
type ArtifactStore interface {
	// Save скачивает srcURL и сохраняет артефакт под ключом key с расширением ext.
	// Возвращает стабильный публичный URL.
	Save(ctx context.Context, srcURL, key, ext string) (publicURL string, err error)
}

// passthroughStore — no-op: возвращает исходный URL как есть (перекладка выключена).
type passthroughStore struct{}

func (passthroughStore) Save(_ context.Context, srcURL, _, _ string) (string, error) {
	return srcURL, nil
}

// LocalArtifactStore — сохраняет артефакты в каталог dir и отдаёт publicBaseURL/{key}.ext.
// Работает с общим volume (переживает пересоздание контейнера BarcodeGen) или с S3,
// смонтированным в этот каталог.
type LocalArtifactStore struct {
	dir           string
	publicBaseURL string
	httpClient    *http.Client
}

// NewLocalArtifactStore создаёт локальную перекладку. dir — куда писать, publicBaseURL —
// базовый URL для публикации (может быть пустым → вернёт file-путь, для dev).
func NewLocalArtifactStore(dir, publicBaseURL string) *LocalArtifactStore {
	return &LocalArtifactStore{
		dir:           dir,
		publicBaseURL: publicBaseURL,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *LocalArtifactStore) Save(ctx context.Context, srcURL, key, ext string) (string, error) {
	if srcURL == "" {
		return "", fmt.Errorf("artifact: empty source URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
	if err != nil {
		return "", fmt.Errorf("artifact: build request: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("artifact: download %s: %w", srcURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("artifact: download %s: status %d", srcURL, resp.StatusCode)
	}

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", fmt.Errorf("artifact: mkdir %q: %w", s.dir, err)
	}
	filename := safeKey(key) + "." + ext
	path := filepath.Join(s.dir, filename)
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("artifact: create %q: %w", path, err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("artifact: write %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("artifact: close %q: %w", path, err)
	}

	if s.publicBaseURL == "" {
		return path, nil
	}
	return fmt.Sprintf("%s/%s", s.publicBaseURL, filename), nil
}

// safeKey превращает произвольный ключ (buildId:index) в безопасное имя файла.
func safeKey(key string) string {
	if key == "" {
		return "barcode"
	}
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:8])
}
