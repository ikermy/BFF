package domain

type SourceBreakdown struct {
	Units     int     `json:"units"`
	Remaining int     `json:"remaining,omitempty"`
	Amount    float64 `json:"amount,omitempty"`
}

type QuoteBreakdown struct {
	Subscription SourceBreakdown `json:"subscription"`
	Credits      SourceBreakdown `json:"credits"`
	Wallet       SourceBreakdown `json:"wallet"`
}

// QuoteResult — ответ на запрос котировки (GET /api/v1/billing/quote, п.12.1 ТЗ).
// UnitPrice возвращается Billing и используется для расчёта shortfall.amountRequired (п.7.3 ТЗ).
type QuoteResult struct {
	CanProcess   bool           `json:"canProcess"`
	Partial      bool           `json:"partial"`
	Requested    int            `json:"requested"`
	AllowedTotal int            `json:"allowedTotal"`
	UnitPrice    float64        `json:"unitPrice"`
	BySource     QuoteBreakdown `json:"bySource"`
	Shortfall    *Shortfall     `json:"shortfall,omitempty"`
}

type Shortfall struct {
	Units          int     `json:"units"`
	AmountRequired float64 `json:"amountRequired"`
}

// BlockRequest — запрос на блокировку средств перед генерацией (п.7.3 ТЗ).
// BySource содержит точную разбивку из quote — BFF говорит Billing откуда списать.
// ВАЖНО: Бонусы начисляются только с wallet.amount (п.7.2 ТЗ).
type BlockRequest struct {
	UserID   string         `json:"userId"`
	Units    int            `json:"units"`
	BySource QuoteBreakdown `json:"bySource"` // разбивка из quote — Subscription/Credits/Wallet
	SagaID   string         `json:"sagaId"`
	BuildID  string         `json:"buildId,omitempty"`
	BatchID  string         `json:"batchId,omitempty"`
}

// Referral — реферальная информация: бонусы начисляются ТОЛЬКО с wallet.amount (п.7.2 ТЗ).
type Referral struct {
	Eligible       bool    `json:"eligible"`
	EligibleAmount float64 `json:"eligibleAmount"` // = wallet.amount
	ReferrerID     string  `json:"referrerId,omitempty"`
}

// GenerateBillingResult — billing-секция в ответе POST /api/v1/barcode/generate (п.12.2 ТЗ).
// Отличается от QuoteResult: содержит итоговую стоимость и referral, без canProcess/partial.
type GenerateBillingResult struct {
	TotalCost        float64        `json:"totalCost"`
	BySource         QuoteBreakdown `json:"bySource"`
	ReferralEligible float64        `json:"referralEligible,omitempty"`
	Referral         *Referral      `json:"referral,omitempty"`
}
