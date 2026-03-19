package events

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/ikermy/BFF/internal/adapters/kafka"
	"github.com/ikermy/BFF/internal/domain"
)

type fakePublisher struct {
	mu       sync.Mutex
	messages map[string][]json.RawMessage
}

func newFakePublisher() *fakePublisher {
	return &fakePublisher{messages: make(map[string][]json.RawMessage)}
}

func (f *fakePublisher) Publish(_ context.Context, t string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages[t] = append(f.messages[t], body)
	return nil
}

func (f *fakePublisher) last(t string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	msgs := f.messages[t]
	if len(msgs) == 0 {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal(msgs[len(msgs)-1], &out)
	return out
}

// TestKafkaPublisher_LogTransaction — проверяет формат сообщения TRANSACTION_LOG (п.11.3 ТЗ).
// TypeScript: { eventType, userId, type, amount, details, timestamp }
func TestKafkaPublisher_LogTransaction(t *testing.T) {
	fake := newFakePublisher()
	pub := NewKafkaPublisher(fake)

	err := pub.LogTransaction(context.Background(), domain.TransactionLog{
		UserID: "u-1",
		Type:   domain.TransactionGeneration,
		Amount: 5.00,
		Details: map[string]any{
			"buildId": "b-1",
			"units":   10,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg := fake.last(kafka.TopicTransHistory)
	if msg == nil {
		t.Fatal("no message published to trans-history.log")
	}

	assertStr(t, msg, "eventType", "TRANSACTION_LOG")
	assertStr(t, msg, "userId", "u-1")
	assertStr(t, msg, "type", "GENERATION")

	if msg["amount"].(float64) != 5.00 {
		t.Errorf("expected amount=5.00, got %v", msg["amount"])
	}
	if msg["timestamp"] == "" || msg["timestamp"] == nil {
		t.Error("expected non-empty timestamp")
	}
	details, ok := msg["details"].(map[string]any)
	if !ok {
		t.Fatalf("expected details object, got %T", msg["details"])
	}
	if details["buildId"] != "b-1" {
		t.Errorf("expected details.buildId=b-1, got %v", details["buildId"])
	}
}

func assertStr(t *testing.T, m map[string]any, key, want string) {
	t.Helper()
	got, ok := m[key].(string)
	if !ok || got != want {
		t.Errorf("field %q: expected %q, got %v", key, want, m[key])
	}
}

// ─── п.11.2 Notifications tests ───────────────────────────────────────────────

// TestKafkaPublisher_SendGenerationComplete — проверяет формат GENERATION_COMPLETE (п.11.2 ТЗ).
// TypeScript: { eventType, userId, channel:"email", data:{barcodeCount,buildId,downloadUrl}, timestamp }
func TestKafkaPublisher_SendGenerationComplete(t *testing.T) {
	fake := newFakePublisher()
	pub := NewKafkaPublisher(fake)

	err := pub.SendGenerationComplete(context.Background(), domain.NotificationRequest{
		UserID:       "u-1",
		BuildID:      "b-1",
		BarcodeCount: 10,
		DownloadURL:  "https://cdn.example.com/b-1.zip",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg := fake.last(kafka.TopicNotifications)
	if msg == nil {
		t.Fatal("no message published to notifications.send")
	}

	assertStr(t, msg, "eventType", "GENERATION_COMPLETE")
	assertStr(t, msg, "userId", "u-1")
	assertStr(t, msg, "channel", "email")

	data, ok := msg["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got: %T", msg["data"])
	}
	assertStr(t, data, "buildId", "b-1")
	assertStr(t, data, "downloadUrl", "https://cdn.example.com/b-1.zip")
	if data["barcodeCount"].(float64) != 10 {
		t.Errorf("expected barcodeCount=10, got %v", data["barcodeCount"])
	}
	if msg["timestamp"] == nil || msg["timestamp"] == "" {
		t.Error("expected non-empty timestamp")
	}
}

// TestKafkaPublisher_SendGenerationError — проверяет формат GENERATION_ERROR (п.11.2 ТЗ).
// TypeScript: { eventType, userId, channel:"push", data:{error,buildId}, timestamp }
func TestKafkaPublisher_SendGenerationError(t *testing.T) {
	fake := newFakePublisher()
	pub := NewKafkaPublisher(fake)

	err := pub.SendGenerationError(context.Background(), domain.ErrorNotificationRequest{
		UserID:  "u-2",
		BuildID: "b-2",
		Error:   "barcodegen unavailable",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg := fake.last(kafka.TopicNotifications)
	if msg == nil {
		t.Fatal("no message published to notifications.send")
	}

	assertStr(t, msg, "eventType", "GENERATION_ERROR")
	assertStr(t, msg, "userId", "u-2")
	assertStr(t, msg, "channel", "push")

	data, ok := msg["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got: %T", msg["data"])
	}
	assertStr(t, data, "buildId", "b-2")
	assertStr(t, data, "error", "barcodegen unavailable")
}
