package legacy

import (
	"fmt"
	"strings"
	"sync"
)

// Мост ревизий: BFF-имя ревизии (US_CA_08292017) ↔ пара полей {DAJ, DDB},
// которую принимает BarcodeGen. BarcodeGen ищет конфиг по findByStateAndRev(values.DAJ,
// values.DDB), а при промахе тихо падает на configs[0] (баркод чужого штата).
// Чтобы тихий fallback никогда не сработал, адаптер валидирует ревизию по явному
// whitelist поддерживаемых пар ДО вызова (отчёт §4.2 п.3).

// revisionPair — разобранная ревизия: state → DAJ, date → DDB.
type revisionPair struct {
	State string // DAJ
	Date  string // DDB (формат MMDDYYYY, как в имени ревизии)
}

// revisionWhitelist — множество поддерживаемых (state, date) пар.
// Заполняется динамически (снятие через POST /barcodes) или вручную через SetSupportedRevisions.
var (
	revMu         sync.RWMutex
	revisionPairs = make(map[revisionPair]bool)
)

// SetSupportedRevisions заменяет whitelist поддерживаемых пар ревизий.
// Вызывается при инициализации адаптера и при обновлении каталога штатов.
func SetSupportedRevisions(pairs []struct{ State, Date string }) {
	revMu.Lock()
	defer revMu.Unlock()
	revisionPairs = make(map[revisionPair]bool, len(pairs))
	for _, p := range pairs {
		if p.State != "" && p.Date != "" {
			revisionPairs[revisionPair{State: strings.ToUpper(p.State), Date: p.Date}] = true
		}
	}
}

// SyncSupportedRevisionsFromConfigs засевает whitelist поддерживаемых пар из имён
// ревизий, которые знает BFF (configs/revisions/*.yaml). Формат имени
// US_<STATE>_<MMDDYYYY> кодирует ровно ту пару {DAJ, DDB}, которую ждёт BarcodeGen
// (findByStateAndRev). Некорректные имена пропускаются. Это самодостаточный источник
// (без зависимости от нестабильного эндпоинта перечисления BarcodeGen).
func SyncSupportedRevisionsFromConfigs(names []string) {
	var pairs []struct{ State, Date string }
	for _, name := range names {
		p, err := parseRevision(name)
		if err != nil {
			continue
		}
		pairs = append(pairs, struct{ State, Date string }{State: p.State, Date: p.Date})
	}
	SetSupportedRevisions(pairs)
}

// parseRevision разбирает имя ревизии формата US_<STATE>_<MMDDYYYY> → revisionPair.
// Возвращает ошибку при неверном формате.
func parseRevision(revision string) (revisionPair, error) {
	parts := strings.Split(revision, "_")
	if len(parts) != 3 || !strings.EqualFold(parts[0], "US") {
		return revisionPair{}, fmt.Errorf("unsupported revision format: %q (want US_<STATE>_<MMDDYYYY>)", revision)
	}
	state := strings.ToUpper(parts[1])
	date := parts[2]
	if len(state) != 2 || len(date) != 8 {
		return revisionPair{}, fmt.Errorf("unsupported revision format: %q (want US_<STATE>_<MMDDYYYY>)", revision)
	}
	return revisionPair{State: state, Date: date}, nil
}

// resolveRevision проверяет ревизию по whitelist и возвращает поля {DAJ, DDB}.
// Если whitelist пуст — валидируется только формат (безопасный режим для этапа 1,
// где whitelist ещё не снят с BarcodeGen).
func resolveRevision(revision string) (map[string]string, error) {
	pair, err := parseRevision(revision)
	if err != nil {
		return nil, err
	}
	revMu.RLock()
	supported := len(revisionPairs) > 0
	ok := revisionPairs[pair]
	revMu.RUnlock()
	if supported && !ok {
		return nil, fmt.Errorf("revision not supported by BarcodeGen: %q", revision)
	}
	return map[string]string{
		"DAJ": pair.State,
		"DDB": pair.Date,
	}, nil
}
