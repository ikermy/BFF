package idempotency

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStore_ReserveAndSet(t *testing.T) {
	store := NewMemoryStore(10 * time.Second)
	ctx := context.Background()

	// Первый Reserve — должен успешно зарезервировать
	ok, err := store.Reserve(ctx, "key-1")
	if err != nil || !ok {
		t.Fatalf("expected Reserve=true, got %v %v", ok, err)
	}

	// Get после Reserve — in-flight, должен вернуть found=false
	_, found, _ := store.Get(ctx, "key-1")
	if found {
		t.Fatal("expected found=false for in-flight key")
	}

	// Второй Reserve того же ключа — должен вернуть false (уже in-flight)
	ok2, err := store.Reserve(ctx, "key-1")
	if err != nil || ok2 {
		t.Fatalf("expected Reserve=false for duplicate, got %v %v", ok2, err)
	}

	// Set — сохраняем готовый ответ
	body := []byte(`{"success":true}`)
	if err := store.Set(ctx, "key-1", body); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Get после Set — должен вернуть found=true с телом
	got, found, err := store.Get(ctx, "key-1")
	if err != nil || !found {
		t.Fatalf("expected found=true after Set, got %v %v", found, err)
	}
	if string(got) != string(body) {
		t.Fatalf("expected %s, got %s", body, got)
	}
}

func TestMemoryStore_TTLExpiry(t *testing.T) {
	store := NewMemoryStore(50 * time.Millisecond)
	ctx := context.Background()

	_, _ = store.Reserve(ctx, "key-ttl")
	_ = store.Set(ctx, "key-ttl", []byte(`{}`))

	time.Sleep(100 * time.Millisecond)

	_, found, _ := store.Get(ctx, "key-ttl")
	if found {
		t.Fatal("expected key to be expired")
	}

	// После истечения TTL Reserve должен снова сработать
	ok, _ := store.Reserve(ctx, "key-ttl")
	if !ok {
		t.Fatal("expected Reserve=true after TTL expiry")
	}
}

func TestMemoryStore_DeleteReleasesInFlight(t *testing.T) {
	store := NewMemoryStore(10 * time.Second)
	ctx := context.Background()

	ok, _ := store.Reserve(ctx, "key-del")
	if !ok {
		t.Fatal("expected Reserve=true")
	}

	// После Reserve ключ in-flight — повторный Reserve невозможен
	ok2, _ := store.Reserve(ctx, "key-del")
	if ok2 {
		t.Fatal("expected Reserve=false while in-flight")
	}

	// Delete снимает маркер
	if err := store.Delete(ctx, "key-del"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Get после Delete — ключ не найден
	_, found, _ := store.Get(ctx, "key-del")
	if found {
		t.Error("expected found=false after Delete")
	}

	// Reserve после Delete — должен снова успешно зарезервировать
	ok3, _ := store.Reserve(ctx, "key-del")
	if !ok3 {
		t.Error("expected Reserve=true after Delete: key must be free")
	}
}

func TestMemoryStore_DeleteFinishedKey(t *testing.T) {
	store := NewMemoryStore(10 * time.Second)
	ctx := context.Background()

	_, _ = store.Reserve(ctx, "key-fin")
	_ = store.Set(ctx, "key-fin", []byte(`{"ok":true}`))

	// Delete должен работать и для завершённых ключей (идемпотентно)
	if err := store.Delete(ctx, "key-fin"); err != nil {
		t.Fatalf("Delete of finished key failed: %v", err)
	}

	_, found, _ := store.Get(ctx, "key-fin")
	if found {
		t.Error("expected found=false after Delete of finished key")
	}
}
