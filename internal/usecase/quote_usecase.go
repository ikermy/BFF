package usecase

import (
	"context"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
)

type QuoteUseCase struct {
	billing      ports.BillingClient
	allowPartial bool
}

func NewQuoteUseCase(billing ports.BillingClient) *QuoteUseCase {
	return &QuoteUseCase{billing: billing, allowPartial: true}
}

func (u *QuoteUseCase) WithPartialSuccessEnabled(enabled bool) *QuoteUseCase {
	u.allowPartial = enabled
	return u
}

// Execute — запрашивает котировку у Billing Service и оркестрирует ответ (п.6.3 ТЗ).
// BFF НЕ содержит бизнес-логики расчётов (п.1.2 ТЗ): решение о доступных средствах
// принимает Billing Service; BFF только интерпретирует результат и собирает ответ.
//
// Результат:
//   - AllowedTotal == 0      → INSUFFICIENT_FUNDS 402 с topUpRequired (п.6.3 ТЗ)
//   - AllowedTotal < units   → QuoteResult{Partial: true, Shortfall: ...}
//   - AllowedTotal >= units  → QuoteResult{Partial: false}
func (u *QuoteUseCase) Execute(ctx context.Context, userID string, units int, revision string) (domain.QuoteResult, error) {
	if units <= 0 {
		return domain.QuoteResult{}, domain.NewValidationError("units must be greater than zero")
	}
	if revision == "" {
		return domain.QuoteResult{}, domain.NewValidationError("revision is required")
	}

	result, err := u.billing.Quote(ctx, userID, units, revision)
	if err != nil {
		return domain.QuoteResult{}, domain.NewBillingError(err)
	}

	// Полный отказ: нет средств совсем → 402 INSUFFICIENT_FUNDS (п.6.3, п.15.1 ТЗ).
	// topUpRequired = сколько нужно пополнить кошелёк чтобы купить все запрошенные units.
	if result.AllowedTotal == 0 {
		return domain.QuoteResult{}, &domain.AppError{
			Code:       domain.ErrCodeInsufficientFunds,
			HTTPStatus: 402,
			Message:    "no funds available",
			Details: map[string]any{
				"topUpRequired": float64(units) * result.UnitPrice,
			},
		}
	}

	if !u.allowPartial && result.AllowedTotal < units {
		amountRequired := float64(units-result.AllowedTotal) * result.UnitPrice
		if result.Shortfall != nil {
			amountRequired = result.Shortfall.AmountRequired
		}
		return domain.QuoteResult{}, &domain.AppError{
			Code:       domain.ErrCodeInsufficientFunds,
			HTTPStatus: 402,
			Message:    "partial success is disabled",
			Details: map[string]any{
				"topUpRequired": amountRequired,
			},
		}
	}

	return result, nil
}
