package billing

import (
	"context"
	"fmt"

	"github.com/ikermy/BFF/internal/domain"
)

// MockClient имитирует Billing Service (п.7 ТЗ).
// Реализует Split Payment waterfall: Subscription → Credits → Wallet (п.7.1 ТЗ).
// В production заменяется на реальный HTTP-клиент к BILLING_URL.
type MockClient struct {
	UnitPrice float64
}

func NewMockClient(unitPrice float64) *MockClient {
	return &MockClient{UnitPrice: unitPrice}
}

// Quote имитирует POST /internal/billing/quote (п.7.3 ТЗ).
// Возвращает разбивку по источникам (Subscription → Credits → Wallet waterfall).
//
// UnitPrice используется как коэффициент доступного баланса (0.0 = нет средств, 1.0 = всё доступно):
//   - UnitPrice <= 0.0  → AllowedTotal = 0 (INSUFFICIENT_FUNDS)
//   - 0.0 < UnitPrice < 1.0 → AllowedTotal = floor(UnitPrice * units) (partial)
//   - UnitPrice >= 1.0  → AllowedTotal = units (полный доступ)
//
// В production UnitPrice — цена за единицу; в mock — ratio для тестирования partial/zero/full flows.
func (c *MockClient) Quote(_ context.Context, _ string, units int, _ string) (domain.QuoteResult, error) {
	// Вычисляем допустимое количество единиц через UnitPrice как ratio.
	allowed := units
	switch {
	case c.UnitPrice <= 0:
		allowed = 0
	case c.UnitPrice < 1.0:
		allowed = int(c.UnitPrice * float64(units))
	}
	partial := allowed < units

	// Split Payment waterfall (п.7.1 ТЗ): сначала подписка, потом кредиты, потом кошелёк.
	sub := min(allowed, 30)
	remaining := allowed - sub
	cred := min(remaining, 20)
	remaining -= cred
	wallet := remaining // всё оставшееся идёт с кошелька

	const unitPrice = 0.50 // фиксированная цена за единицу для расчёта суммы
	result := domain.QuoteResult{
		CanProcess:   allowed > 0,
		Partial:      partial,
		Requested:    units,
		AllowedTotal: allowed,
		UnitPrice:    unitPrice,
		BySource: domain.QuoteBreakdown{
			Subscription: domain.SourceBreakdown{Units: sub, Remaining: max(30-sub, 0)},
			Credits:      domain.SourceBreakdown{Units: cred, Remaining: max(20-cred, 0)},
			// Wallet.Amount — именно эта сумма участвует в реферальной программе (п.7.2 ТЗ).
			Wallet: domain.SourceBreakdown{Units: wallet, Amount: float64(wallet) * unitPrice},
		},
	}

	if partial {
		result.Shortfall = &domain.Shortfall{
			Units:          units - allowed,
			AmountRequired: float64(units-allowed) * unitPrice,
		}
	}
	return result, nil
}

// Block имитирует POST /internal/billing/block (п.7.3 ТЗ).
// req.BySource содержит точную разбивку — BFF говорит Billing откуда списать (Split Payment).
// units=0 допускается для бесплатного редактирования (п.10.1 ТЗ).
func (c *MockClient) Block(_ context.Context, req domain.BlockRequest) error {
	if req.SagaID == "" {
		return fmt.Errorf("sagaID is required for block")
	}
	// units=0 разрешено для free edit (по ТЗ 10.1: POST /block {credits: 0})
	if req.Units < 0 {
		return fmt.Errorf("units must be >= 0")
	}
	return nil
}

// Capture — списывает successCount единиц по sagaID (п.14.4 ТЗ).
func (c *MockClient) Capture(_ context.Context, sagaID string, units int) error {
	if sagaID == "" || units <= 0 {
		return fmt.Errorf("invalid capture request")
	}
	return nil
}

// Release — разблокирует units единиц по sagaID (п.14.4 ТЗ).
func (c *MockClient) Release(_ context.Context, sagaID string, units int) error {
	if sagaID == "" || units <= 0 {
		return fmt.Errorf("invalid release request")
	}
	return nil
}

// BlockBatch — блокирует средства для bulk-задачи (Bulk_Service_TZ п.4).
func (c *MockClient) BlockBatch(_ context.Context, _ string, count int, batchID string) ([]string, error) {
	if count <= 0 || batchID == "" {
		return nil, fmt.Errorf("invalid block-batch request")
	}
	ids := make([]string, 0, count)
	for i := 0; i < count; i++ {
		ids = append(ids, fmt.Sprintf("%s-tx-%d", batchID, i+1))
	}
	return ids, nil
}
