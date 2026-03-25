package idempotency

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// inFlightMarker — маркер «ключ зарезервирован, ответ ещё не готов».
// TypeScript: checkOrSet(key, null) → redis.set(key, JSON.stringify(null), 'EX', ttl).
// Выбран \x00 чтобы отличаться от любого валидного JSON-тела.
const inFlightMarker = "\x00"

// keyPrefix — префикс ключей в Redis (п.14.1 ТЗ).
// TypeScript: `idempotency:${key}`
const keyPrefix = "idempotency:"

// RedisStore — production-реализация IdempotencyStore (п.14.1 ТЗ).
// В development используется MemoryStore.
//
// Redis-операции:
//
//	Reserve → SET idempotency:{key} "\x00" EX {ttl} NX
//	Set     → SET idempotency:{key} {body}  EX {ttl}
//	Get     → GET idempotency:{key} → "\x00"=in-flight, JSON=готово, nil=не найден
type RedisStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisStore создаёт Redis-хранилище идемпотентности.
// redisURL берётся из cfg.Redis.URL (REDIS_URL, п.17.1 ТЗ).
// Формат URL: redis://[:password@]host[:port][/db-number]
func NewRedisStore(redisURL string, ttl time.Duration) (*RedisStore, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("idempotency: parse redis url %q: %w", redisURL, err)
	}
	return &RedisStore{
		client: redis.NewClient(opts),
		ttl:    ttl,
	}, nil
}

// Get возвращает (body, true, nil) если ключ завершён с готовым ответом.
// Возвращает (nil, false, nil) если ключ не найден или in-flight.
// TypeScript: existing = await redis.get(`idempotency:${key}`)
func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	val, err := s.client.Get(ctx, keyPrefix+key).Bytes()
	if err == redis.Nil {
		return nil, false, nil // ключ не найден
	}
	if err != nil {
		return nil, false, fmt.Errorf("idempotency: redis get: %w", err)
	}
	if string(val) == inFlightMarker {
		return nil, false, nil // ключ зарезервирован, ответ ещё не готов
	}
	return val, true, nil
}

// Reserve резервирует ключ через SET NX (п.14.2 ТЗ).
// Возвращает true если ключ успешно зарезервирован (первый запрос).
// Возвращает false если ключ уже существует (параллельный или повторный запрос).
// TypeScript: redis.set(key, JSON.stringify(null), 'EX', ttl) — здесь эквивалент через NX.
func (s *RedisStore) Reserve(ctx context.Context, key string) (bool, error) {
	res, err := s.client.SetArgs(ctx, keyPrefix+key, inFlightMarker, redis.SetArgs{
		Mode: "NX",
		TTL:  s.ttl,
	}).Result()
	if err != nil && err != redis.Nil {
		return false, fmt.Errorf("idempotency: redis set nx: %w", err)
	}
	return res == "OK", nil
}

// Set сохраняет готовый ответ (перезаписывает in-flight маркер).
// TypeScript: redis.set(key, JSON.stringify(response), 'EX', ttl)
func (s *RedisStore) Set(ctx context.Context, key string, body []byte) error {
	if err := s.client.Set(ctx, keyPrefix+key, body, s.ttl).Err(); err != nil {
		return fmt.Errorf("idempotency: redis set: %w", err)
	}
	return nil
}

// Delete удаляет in-flight маркер, освобождая ключ для повторного запроса.
// Вызывается middleware при ошибке хендлера (не-2xx): без этого маркер
// блокирует все ретраи с тем же X-Idempotency-Key до истечения TTL.
func (s *RedisStore) Delete(ctx context.Context, key string) error {
	if err := s.client.Del(ctx, keyPrefix+key).Err(); err != nil {
		return fmt.Errorf("idempotency: redis del: %w", err)
	}
	return nil
}
