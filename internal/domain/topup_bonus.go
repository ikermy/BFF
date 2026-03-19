package domain

// TopupBonusTier — уровень бонуса за пополнение (п.14.7 ТЗ).
// MaxAmount == nil означает «без верхней границы».
type TopupBonusTier struct {
	MinAmount    float64  `json:"minAmount"`
	MaxAmount    *float64 `json:"maxAmount"`
	BonusPercent float64  `json:"bonusPercent"`
}

// TopupBonusConfig — конфигурация бонусов за пополнение (PUT /admin/config/topup-bonus).
type TopupBonusConfig struct {
	Enabled bool             `json:"enabled"`
	Tiers   []TopupBonusTier `json:"tiers"`
}
