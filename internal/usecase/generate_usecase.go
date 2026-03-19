package usecase

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/metrics"
	"github.com/ikermy/BFF/internal/ports"
)

// Retry-параметры для BarcodeGen (п.14.3 ТЗ).
const maxBarcodeGenRetries = 3

var barcodeGenRetryDelays = []time.Duration{
	1 * time.Second,
	3 * time.Second,
	5 * time.Second,
}

// Quoter — минимальный интерфейс для получения котировки.
// Позволяет GenerateUseCase не зависеть от конкретного *QuoteUseCase.
type Quoter interface {
	Execute(ctx context.Context, userID string, units int, revision string) (domain.QuoteResult, error)
}

type GenerateUseCase struct {
	billing       ports.BillingClient
	barcode       ports.BarcodeGenClient
	events        ports.EventPublisher
	quoter        Quoter
	chain         *ChainExecutor // может быть nil — тогда цепочка не выполняется
	notifications ports.NotificationsPublisher
	transHistory  ports.TransHistoryPublisher
	ai            ports.AIClient
	revisionStore ports.RevisionConfigStore // для validateMinimumSet (п.5.2 ТЗ)
	allowPartial  bool
}

func NewGenerateUseCase(
	billing ports.BillingClient,
	barcode ports.BarcodeGenClient,
	events ports.EventPublisher,
	quoter Quoter,
) *GenerateUseCase {
	return &GenerateUseCase{billing: billing, barcode: barcode, events: events, quoter: quoter, allowPartial: true}
}

// WithChainExecutor подключает ChainExecutor к GenerateUseCase (п.4 ТЗ).
func (u *GenerateUseCase) WithChainExecutor(chain *ChainExecutor) *GenerateUseCase {
	u.chain = chain
	return u
}

// WithNotifications подключает Notifications publisher (п.11.2 ТЗ).
func (u *GenerateUseCase) WithNotifications(n ports.NotificationsPublisher) *GenerateUseCase {
	u.notifications = n
	return u
}

// WithTransHistory подключает TransHistory publisher (п.11.3 ТЗ).
func (u *GenerateUseCase) WithTransHistory(t ports.TransHistoryPublisher) *GenerateUseCase {
	u.transHistory = t
	return u
}

// WithAI подключает AI Service клиент (п.9 ТЗ).
func (u *GenerateUseCase) WithAI(ai ports.AIClient) *GenerateUseCase {
	u.ai = ai
	return u
}

// WithRevisionStore подключает RevisionConfigStore для validateMinimumSet (п.5.2 ТЗ).
func (u *GenerateUseCase) WithRevisionStore(store ports.RevisionConfigStore) *GenerateUseCase {
	u.revisionStore = store
	return u
}

func (u *GenerateUseCase) WithPartialSuccessEnabled(enabled bool) *GenerateUseCase {
	u.allowPartial = enabled
	return u
}

// Execute — оркестрирует quote → chain → block → generate → capture/release → publish.
// Реализует Compensating Transactions (п.14.4 ТЗ):
// при частичном сбое BarcodeGen — Capture успешных, Release неудачных.
func (u *GenerateUseCase) Execute(ctx context.Context, userID string, req domain.GenerateRequest) (domain.GenerateResponse, error) {
	if req.Units <= 0 {
		return domain.GenerateResponse{}, domain.NewValidationError("units must be greater than zero")
	}
	if req.Revision == "" {
		return domain.GenerateResponse{}, domain.NewValidationError("revision is required")
	}

	// Валидация минимального набора обязательных входных полей (п.5.2 ТЗ).
	// Проверяем только присутствие полей — бизнес-правила проверяет BarcodeGen.
	if u.revisionStore != nil {
		cfg, cfgErr := u.revisionStore.GetConfig(ctx, req.Revision)
		if cfgErr != nil {
			return domain.GenerateResponse{}, domain.NewValidationError("revision not found: " + req.Revision)
		}
		if appErr := validateMinimumSet(cfg, req.Fields); appErr != nil {
			return domain.GenerateResponse{}, appErr
		}
	}

	quote, err := u.quoter.Execute(ctx, userID, req.Units, req.Revision)
	if err != nil {
		// Сохраняем AppError как есть (например, INSUFFICIENT_FUNDS 402 из QuoteUseCase).
		// Оборачиваем только «сырые» ошибки (сетевые, неизвестные) как BILLING_ERROR.
		var appErr *domain.AppError
		if errors.As(err, &appErr) {
			return domain.GenerateResponse{}, appErr
		}
		return domain.GenerateResponse{}, domain.NewBillingError(err)
	}
	// AllowedTotal == 0 теперь обрабатывается в QuoteUseCase (возвращает INSUFFICIENT_FUNDS).
	// Здесь оставляем только partial-check.
	if quote.Partial && !req.Confirmed {
		return domain.GenerateResponse{}, &domain.AppError{
			Code:       domain.ErrCodePartialFunds,
			HTTPStatus: 200,
			Message:    fmt.Sprintf("only %d of %d units available, set confirmed=true to proceed", quote.AllowedTotal, req.Units),
		}
	}
	if quote.Partial && !u.allowPartial {
		amountRequired := float64(req.Units-quote.AllowedTotal) * quote.UnitPrice
		if quote.Shortfall != nil {
			amountRequired = quote.Shortfall.AmountRequired
		}
		return domain.GenerateResponse{}, &domain.AppError{
			Code:       domain.ErrCodeInsufficientFunds,
			HTTPStatus: 402,
			Message:    "partial success is disabled",
			Details: map[string]any{
				"topUpRequired": amountRequired,
			},
		}
	}

	generateCount := req.Units
	if quote.Partial {
		generateCount = quote.AllowedTotal
	}

	// Выполняем цепочку расчётов полей (п.4 ТЗ), если ChainExecutor подключён
	resolvedFields := req.Fields
	if resolvedFields == nil {
		resolvedFields = make(map[string]any)
	}

	var (
		computed []string
		skipped  []string
	)

	if u.chain != nil && len(req.Fields) > 0 {
		chainResult, chainErr := u.chain.Execute(ctx, req.Revision, req.Fields)
		if chainErr != nil {
			return domain.GenerateResponse{}, chainErr
		}
		resolvedFields = chainResult.Fields
		computed = chainResult.Computed
		skipped = chainResult.Skipped
	}

	// sagaID вычисляется один раз — используется и для AI (SagaID в запросе) и для Billing.Block.
	sagaID := fmt.Sprintf("saga-%s-%s", req.BuildID, req.BatchID)

	// AI Service: генерация подписи и фото (п.9.3 ТЗ).
	// Вызов СИНХРОННЫЙ — ошибка при явном флаге прерывает flow.
	if u.ai != nil {
		_, hasSignature := resolvedFields["signatureUrl"]

		// Условие 1: generateSignature=true — явный запрос → ошибка AI критична.
		// Условие 2: signatureUrl не указан → авто-триггер, ошибка AI не критична.
		if req.GenerateSignature || !hasSignature {
			fullName := fmt.Sprintf("%v %v", resolvedFields["firstName"], resolvedFields["lastName"])
			sigResp, sigErr := u.ai.GenerateSignature(ctx, domain.AISignatureRequest{
				UserID:   userID,
				SagaID:   sagaID,
				FullName: fullName,
				Style:    req.SignatureStyle,
			})
			if sigErr != nil {
				if req.GenerateSignature {
					// Явный флаг — AI обязателен, прерываем
					return domain.GenerateResponse{}, &domain.AppError{
						Code:       domain.ErrCodeBarcodeGenError,
						HTTPStatus: 503,
						Message:    "AI signature service unavailable: " + sigErr.Error(),
					}
				}
				// Авто-триггер — продолжаем без подписи
			} else {
				resolvedFields["signatureUrl"] = sigResp.ImageURL
			}
		}

		// Условие 3: generatePhoto=true — явный запрос → ошибка AI критична.
		if req.GeneratePhoto {
			photoResp, photoErr := u.ai.GeneratePhoto(ctx, domain.AIPhotoRequest{
				UserID:      userID,
				SagaID:      sagaID,
				Description: req.PhotoDescription,
				Gender:      req.Gender,
				Age:         req.Age,
			})
			if photoErr != nil {
				return domain.GenerateResponse{}, &domain.AppError{
					Code:       domain.ErrCodeBarcodeGenError,
					HTTPStatus: 503,
					Message:    "AI photo service unavailable: " + photoErr.Error(),
				}
			}
			resolvedFields["photoUrl"] = photoResp.ImageURL
		}
	}

	// Block: передаём точную разбивку bySource из quote (Split Payment п.7.1 ТЗ).
	// Billing списывает строго по источникам: Subscription → Credits → Wallet.
	if err := u.billing.Block(ctx, domain.BlockRequest{
		UserID:   userID,
		Units:    generateCount,
		BySource: quote.BySource,
		SagaID:   sagaID,
		BuildID:  req.BuildID,
		BatchID:  req.BatchID,
	}); err != nil {
		return domain.GenerateResponse{}, domain.NewBillingError(err)
	}

	// Генерируем все баркоды с retry (п.14.3 ТЗ) — НЕ падаем при первой ошибке.
	// Накапливаем успешные и считаем неудачные для Capture/Release (п.14.4 ТЗ).
	barcodes := make([]domain.BarcodeItem, 0, generateCount)
	failedCount := 0
	for i := 0; i < generateCount; i++ {
		item, genErr := generateWithRetry(ctx, u.barcode, req, resolvedFields, buildBarcodeGenIdempotencyKey(req.IdempotencyKey, i))
		if genErr != nil {
			failedCount++
			continue
		}
		barcodes = append(barcodes, item)
	}

	successCount := len(barcodes)

	// Все упали — Release всего заблокированного, вернуть ошибку.
	if successCount == 0 {
		if releaseErr := u.billing.Release(ctx, sagaID, generateCount); releaseErr != nil {
			// Release упал — сага в inconsistent state: средства заблокированы, баркодов нет.
			// Возвращаем BILLING_ERROR 503 (п.15.1 ТЗ): клиент и мониторинг получают сигнал,
			// что проблема на стороне Billing, а не только BarcodeGen.
			return domain.GenerateResponse{}, domain.NewBillingError(
				fmt.Errorf("all generations failed AND release failed (sagaID=%s): %w", sagaID, releaseErr),
			)
		}
		return domain.GenerateResponse{}, domain.NewBarcodeGenError(
			fmt.Errorf("all %d generation attempts failed", generateCount),
		)
	}

	// Capture только успешных (п.14.4 ТЗ).
	if err := u.billing.Capture(ctx, sagaID, successCount); err != nil {
		return domain.GenerateResponse{}, domain.NewBillingError(err)
	}

	// Release неудачных + событие partial_completed (п.14.4 ТЗ).
	// partial_completed публикуется в двух случаях:
	// 1. Billing вернул quota.Partial=true — пользователь получил меньше, чем запросил из-за баланса.
	// 2. BarcodeGen упал на части запроса (failedCount > 0).
	// В обоих случаях сага считается "частично завершённой".
	isPartialOutcome := failedCount > 0 || quote.Partial
	if isPartialOutcome {
		// FIXME
		// Capture уже выполнен — клиент получит M баркодов независимо от результата Release.
		// ТЗ не определяет поведение при сбое компенсирующей транзакции Release в случае
		// partial success, поэтому ошибку намеренно игнорируем.
		if failedCount > 0 {
			_ = u.billing.Release(ctx, sagaID, failedCount)
		}
		// FIXME
		// Capture/Release уже выполнены — возврат ошибки клиенту не изменит исход саги.
		// ТЗ не определяет поведение при сбое публикации billing.saga.partial_completed.
		// Намеренно игнорируем.
		_ = u.events.PublishPartialCompleted(ctx, domain.PartialCompletedEvent{
			SagaID:        sagaID,
			SuccessUnits:  successCount,
			ReleasedUnits: failedCount,
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
		})
		metrics.PartialSuccessTotal.Inc() // п.17 ТЗ
	} else {
		// FIXME
		// Capture выполнен — возврат ошибки клиенту не изменит исход саги.
		// ТЗ не определяет поведение при сбое публикации billing.saga.completed.
		// Намеренно игнорируем.
		_ = u.events.PublishSagaCompleted(ctx, sagaID)
	}

	walletAmount := quote.BySource.Wallet.Amount
	billing := domain.GenerateBillingResult{
		TotalCost:        float64(successCount) * walletAmount / float64(max(quote.BySource.Wallet.Units, 1)),
		BySource:         quote.BySource,
		ReferralEligible: walletAmount,
		// Реферальные бонусы начисляются ТОЛЬКО с wallet.amount (п.7.2 ТЗ).
		Referral: &domain.Referral{
			Eligible:       walletAmount > 0,
			EligibleAmount: walletAmount,
		},
	}

	// FIXME
	// Публикуем barcode.generated для каждого успешного баркода (п.10.3 ТЗ).
	// Consumer — History Service; записывает в историю транзакций.
	// Billing Capture уже выполнен, баркоды возвращаются клиенту — возврат ошибки не имеет смысла.
	// ТЗ не определяет поведение при сбое публикации barcode.generated.
	// Намеренно игнорируем.
	now := time.Now().UTC().Format(time.RFC3339)
	for _, bc := range barcodes {
		_ = u.events.PublishBarcodeGenerated(ctx, domain.BarcodeGeneratedEvent{
			UserID:      userID,
			BuildID:     req.BuildID,
			BatchID:     req.BatchID,
			Revision:    req.Revision,
			BarcodeType: req.BarcodeType,
			BarcodeURL:  bc.URL,
			Fields:      resolvedFields,
			Billing: &domain.BarcodeGeneratedBilling{
				TotalCost: billing.TotalCost / float64(successCount),
				BySource:  quote.BySource,
			},
			CreatedAt: now,
		})
	}

	resp := domain.GenerateResponse{
		Success:  true,
		BuildID:  req.BuildID,
		BatchID:  req.BatchID,
		Barcodes: barcodes,
		Computed: computed,
		Skipped:  skipped,
		Billing:  billing,
	}

	// FIXME
	// Уведомление об успехе / ошибке (п.11.2 ТЗ, топик notifications.send).
	// Billing Capture выполнен, баркоды возвращаются клиенту — сбой уведомления
	// не отменяет факт генерации. ТЗ не определяет поведение при сбое публикации.
	// Намеренно игнорируем.
	if u.notifications != nil {
		if failedCount > 0 {
			_ = u.notifications.SendGenerationError(ctx, domain.ErrorNotificationRequest{
				UserID:  userID,
				Error:   fmt.Sprintf("partial generation: %d failed out of %d", failedCount, generateCount),
				BuildID: req.BuildID,
			})
		} else {
			_ = u.notifications.SendGenerationComplete(ctx, domain.NotificationRequest{
				UserID:       userID,
				BarcodeCount: successCount,
				BuildID:      req.BuildID,
			})
		}
	}

	// FIXME
	// Логирование транзакции (п.11.3 ТЗ, топик trans-history.log).
	// Аналогично уведомлениям: сбой лога не отменяет факт генерации и оплаты.
	// ТЗ не определяет поведение при сбое публикации. Намеренно игнорируем.
	if u.transHistory != nil {
		_ = u.transHistory.LogTransaction(ctx, domain.TransactionLog{
			UserID: userID,
			Type:   domain.TransactionGeneration,
			Amount: billing.TotalCost,
			Details: map[string]any{
				"buildId":      req.BuildID,
				"batchId":      req.BatchID,
				"successUnits": successCount,
				"failedUnits":  failedCount,
			},
		})
	}

	return resp, nil
}

// generateWithRetry — вызывает BarcodeGen.Generate с экспоненциальным backoff (п.14.3 ТЗ).
// Повторяет попытки только при 5xx / сетевых ошибках.
func generateWithRetry(
	ctx context.Context,
	client ports.BarcodeGenClient,
	req domain.GenerateRequest,
	fields map[string]any,
	idempotencyKey string,
) (domain.BarcodeItem, error) {
	var lastErr error
	for attempt := 0; attempt < maxBarcodeGenRetries; attempt++ {
		item, err := generateBarcode(ctx, client, req, fields, idempotencyKey)
		if err == nil {
			metrics.BarcodeGenCallsTotal.WithLabelValues("success").Inc() // п.17 ТЗ
			return item, nil
		}
		lastErr = err
		if !isRetryableBarcodeGenError(err) {
			metrics.BarcodeGenCallsTotal.WithLabelValues("error").Inc() // п.17 ТЗ
			return domain.BarcodeItem{}, err
		}
		metrics.BarcodeGenCallsTotal.WithLabelValues("retry").Inc() // п.17 ТЗ
		if attempt < maxBarcodeGenRetries-1 {
			select {
			case <-ctx.Done():
				return domain.BarcodeItem{}, ctx.Err()
			case <-time.After(barcodeGenRetryDelays[attempt]):
			}
		}
	}
	metrics.BarcodeGenCallsTotal.WithLabelValues("error").Inc() // п.17 ТЗ
	return domain.BarcodeItem{}, lastErr
}

func generateBarcode(
	ctx context.Context,
	client ports.BarcodeGenClient,
	req domain.GenerateRequest,
	fields map[string]any,
	idempotencyKey string,
) (domain.BarcodeItem, error) {
	switch strings.ToLower(req.BarcodeType) {
	case "", "pdf417":
		resp, err := client.GeneratePDF417(ctx, domain.GeneratePDF417Request{
			Revision:       req.Revision,
			BuildID:        req.BuildID,
			BatchID:        req.BatchID,
			Fields:         fields,
			IdempotencyKey: idempotencyKey,
		})
		if err != nil {
			return domain.BarcodeItem{}, err
		}
		return domain.BarcodeItem{URL: resp.BarcodeURL, Format: resp.Format}, nil
	case "code128":
		data, _ := fields["data"].(string)
		if strings.TrimSpace(data) == "" {
			return domain.BarcodeItem{}, domain.NewValidationError("data field is required for code128 generation")
		}
		resp, err := client.GenerateCode128(ctx, domain.GenerateCode128Request{
			Data:           data,
			BuildID:        req.BuildID,
			IdempotencyKey: idempotencyKey,
		})
		if err != nil {
			return domain.BarcodeItem{}, err
		}
		return domain.BarcodeItem{URL: resp.BarcodeURL, Format: resp.Format}, nil
	default:
		return domain.BarcodeItem{}, domain.NewValidationError("unsupported barcodeType: " + req.BarcodeType)
	}
}

func buildBarcodeGenIdempotencyKey(base string, index int) string {
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", base, index)
}

func isRetryableBarcodeGenError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "status 500") ||
		strings.Contains(msg, "status 502") ||
		strings.Contains(msg, "status 503") ||
		strings.Contains(msg, "status 504") ||
		strings.Contains(strings.ToUpper(msg), "ECONNREFUSED")
}

// validateMinimumSet проверяет минимальный набор обязательных входных полей (п.5.2 ТЗ).
// НЕ проверяет бизнес-правила — это делает BarcodeGen!
// Использует hasUserValue из chain_executor.go (одинаковая семантика «заполнено»).
func validateMinimumSet(cfg domain.RevisionConfig, input map[string]any) *domain.AppError {
	missing := make([]string, 0)
	for _, field := range cfg.RequiredInputFields {
		if !hasUserValue(input, field) {
			missing = append(missing, field)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return domain.NewRequiredFieldsError(missing)
}
