package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// StableGenerationID возвращает детерминированный ID генерации для одного юнита
// запроса (ПЛАН §3.3). Один и тот же (idempotencyKey, index) всегда даёт один ID,
// который BFF передаёт в BarcodeGen как renderKey — поэтому retry/replay при
// потере кэша идемпотентности возвращает тот же barcode, а не создаёт дубль.
//
// Формат — UUID-подобный (version 5, name-based) из sha256.
func StableGenerationID(idempotencyKey string, index int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("bff-generation:%s:%d", idempotencyKey, index)))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x50 // version 5
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	s := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", s[0:8], s[8:12], s[12:16], s[16:20], s[20:32])
}
