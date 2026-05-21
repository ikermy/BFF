package idempotency

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// editLockTTL — TTL эксклюзивного лока на редактирование баркода.
// 30 секунд достаточно для полного цикла: Block → GeneratePDF417 → PublishBarcodeEdited.
// По истечении TTL Redis автоматически снимает лок — защита от Zombie Lock.
const editLockTTL = 30 * time.Second

// editLockPrefix — префикс Redis-ключей для редактирования (отделён от idempotency:).
const editLockPrefix = "edit-lock:"

// RedisEditLocker — production-реализация EditLocker на основе Redis SetNX (п.10.1 ТЗ).
// Предотвращает Race Condition: злоумышленник не может использовать бесплатное
// редактирование дважды, посылая запросы с разными Idempotency-ключами.
type RedisEditLocker struct {
	client *redis.Client
}

// NewRedisEditLocker создаёт Redis-локер для edit-операций.
// redisURL берётся из cfg.Redis.URL (REDIS_URL).
func NewRedisEditLocker(redisURL string) (*RedisEditLocker, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("edit locker: parse redis url %q: %w", redisURL, err)
	}
	return &RedisEditLocker{client: redis.NewClient(opts)}, nil
}

// TryLock атомарно захватывает блокировку для barcodeID через SET NX EX.
// Возвращает true если лок захвачен, false если уже занят параллельным запросом.
func (l *RedisEditLocker) TryLock(ctx context.Context, barcodeID string) (bool, error) {
	res, err := l.client.SetArgs(ctx, editLockPrefix+barcodeID, "1", redis.SetArgs{
		Mode: "NX",
		TTL:  editLockTTL,
	}).Result()
	if err != nil && err != redis.Nil {
		return false, fmt.Errorf("edit locker: try lock %q: %w", barcodeID, err)
	}
	return res == "OK", nil
}

// Unlock освобождает блокировку для barcodeID.
// Вызывается через defer после завершения обработки (успех или ошибка).
func (l *RedisEditLocker) Unlock(ctx context.Context, barcodeID string) error {
	if err := l.client.Del(ctx, editLockPrefix+barcodeID).Err(); err != nil {
		return fmt.Errorf("edit locker: unlock %q: %w", barcodeID, err)
	}
	return nil
}
