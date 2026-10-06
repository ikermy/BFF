// Package revisionstest предоставляет тестовый helper для загрузки
// version-controlled профилей из каталога configs/revisions репозитория.
//
// Профили больше не содержат hardcoded определений в NewMemoryStore(), поэтому
// тестам, которым нужен реальный профиль (например US_CA_08292017), нужно
// загрузить его из репозитория, как это делает production (LoadFromDir).
package revisionstest

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ikermy/BFF/internal/adapters/revisions"
)

// Load создаёт MemoryStore и загружает в него все профили из
// <repo-root>/configs/revisions. Путь вычисляется относительно исходника helper'а,
// поэтому не зависит от рабочего каталога вызывающего пакета.
func Load() (*revisions.MemoryStore, error) {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "configs", "revisions")
	s := revisions.NewMemoryStore()
	if err := s.LoadFromDir(dir); err != nil {
		return nil, err
	}
	return s, nil
}

// MustLoad — Load с t.Fatalf при ошибке (удобно для тестов).
func MustLoad(tb testing.TB) *revisions.MemoryStore {
	tb.Helper()
	s, err := Load()
	if err != nil {
		tb.Fatalf("revisionstest.Load: %v", err)
	}
	return s
}
