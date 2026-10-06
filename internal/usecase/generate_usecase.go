package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/metrics"
	"github.com/ikermy/BFF/internal/ports"
)

// Имена accepted derive checkpoints (ПЛАН §3.3).
const (
	deriveCheckpointName = "derive"
	renderCheckpointName = "render"
)

// deriveCheckpoint — принятый результат derive-цепочки, сохраняемый под
// X-Idempotency-Key. Повторная попытка (после сбоя до terminal-ответа)
// переиспользует его вместо повторного random/calculate.
type deriveCheckpoint struct {
	RequestHash string         `json:"requestHash"`
	Fields      map[string]any `json:"fields"`
	Computed    []string       `json:"computed"`
	Skipped     []string       `json:"skipped"`
}

// renderCheckpoint — принятый результат render (stable generationId + barcodes),
// сохраняемый до Billing.Capture для status reconciliation.
type renderCheckpoint struct {
	GenerationIDs []string             `json:"generationIds"`
	Barcodes      []domain.BarcodeItem `json:"barcodes"`
}

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
	idem          ports.IdempotencyStore    // accepted derive checkpoints (ПЛАН §3.3)
	reconciler    ports.RenderReconciler    // reconciliation внутреннего render registry (ПЛАН §B.5)
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

// WithIdempotencyStore подключает хранилище для accepted derive checkpoints
// (ПЛАН §3.3). Без него checkpoints не сохраняются/не переиспользуются.
func (u *GenerateUseCase) WithIdempotencyStore(store ports.IdempotencyStore) *GenerateUseCase {
	u.idem = store
	return u
}

// WithRenderReconciler подключает reconciliation внутреннего render registry
// (ПЛАН §B.5): ambiguous timeout разрешается без повторного encoder run.
func (u *GenerateUseCase) WithRenderReconciler(r ports.RenderReconciler) *GenerateUseCase {
	u.reconciler = r
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

	// authenticated ownerId для internal render (BarcodeGen) — из контекста.
	ctx = domain.WithUserID(ctx, userID)

	// Валидация минимального набора обязательных входных полей (п.5.2 ТЗ).
	// Проверяем только присутствие полей — бизнес-правила проверяет BarcodeGen.
	var cfg domain.RevisionConfig
	if u.revisionStore != nil {
		c, cfgErr := u.revisionStore.GetConfig(ctx, req.Revision)
		if cfgErr != nil {
			return domain.GenerateResponse{}, domain.NewValidationError("revision not found: " + req.Revision)
		}
		cfg = c
		if appErr := validateMinimumSet(cfg, req.Fields); appErr != nil {
			return domain.GenerateResponse{}, appErr
		}
		// ПЛАН §3.3 (шаг 1 auto / шаг 2 prepared): choice fallbacks + closed-choice
		// validation выполняются до quote — единый finalizer для всех режимов.
		if len(cfg.Fields) > 0 {
			baseFields := cloneFields(req.Fields)
			applyChoiceFallbacks(cfg.Fields, baseFields)
			if appErr := validateClosedChoicesFromFields(cfg.Fields, baseFields); appErr != nil {
				return domain.GenerateResponse{}, appErr
			}
			req.Fields = baseFields
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

	if req.Mode != "prepared" {
		// ПЛАН §3.3: если для этого X-Idempotency-Key already accepted derive
		// checkpoint, переиспользуем его — повторный random/calculate не выполняем.
		if cp, ok := u.loadDeriveCheckpoint(ctx, req); ok {
			resolvedFields = cp.Fields
			computed = cp.Computed
			skipped = cp.Skipped
		} else if u.chain != nil && len(req.Fields) > 0 {
			chainResult, chainErr := u.chain.ExecuteGrouped(ctx, req.Revision, req.Fields)
			if chainErr != nil {
				return domain.GenerateResponse{}, chainErr
			}
			resolvedFields = chainResult.Fields
			computed = chainResult.Computed
			skipped = chainResult.Skipped
			u.saveDeriveCheckpoint(ctx, req, resolvedFields, computed, skipped)
		}
	}

	// sagaID вычисляется один раз — используется и для AI (SagaID в запросе) и для Billing.Block.
	// userID включён явно: два разных пользователя с одинаковым buildID+batchID
	// не получат одинаковый sagaID. Уникальность buildID+batchID в рамках одного
	// пользователя — ответственность клиента (он генерирует эти поля).
	sagaID := fmt.Sprintf("saga-%s-%s-%s", userID, req.BuildID, req.BatchID)

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
	//
	// engineFields (renderValues) отделены от public/AI/history данных (ПЛАН §3.3):
	// только allowlisted engine-поля профиля, затем public→engine маппинг.
	engineFields := resolvedFields
	if strings.EqualFold(req.BarcodeType, "") || strings.EqualFold(req.BarcodeType, "pdf417") {
		engineFields = filterRenderValues(cfg.RenderAllowlist, resolvedFields)
	}
	engineFields = domain.NormalizeEngineFields(engineFields)
	// static-константы профиля (QQQ/DCA/DBC/…), если не заданы пользователем/derive.
	domain.ApplyEngineDefaults(engineFields, cfg.Defaults)

	// requiredRenderFields: обязательные engine-поля перед render (ПЛАН §3.5).
	if missing := missingRenderFields(cfg.RequiredRenderFields, engineFields); len(missing) > 0 {
		return domain.GenerateResponse{}, domain.NewRequiredFieldsError(missing)
	}

	// prepared: валидируем готовую пару дат (без перегенерации) → 422 (МИКРО_ТЗ дат).
	if req.Mode == "prepared" {
		if appErr := validatePreparedDates(engineFields, cfg.RevisionEffectiveDate); appErr != nil {
			return domain.GenerateResponse{}, appErr
		}
	}

	// Country-aware кодирование дат (US→MMDDYYYY, CA→YYYYMMDD), рост→см и
	// guard возраста <16 (doc BFF_ПОЛЯ_И_ЦЕПОЧКИ_ПО_РЕВИЗИЯМ).
	if appErr := applyRenderTransforms(engineFields, cfg); appErr != nil {
		return domain.GenerateResponse{}, appErr
	}

	barcodes := make([]domain.BarcodeItem, 0, generateCount)
	failedCount := 0
	for i := 0; i < generateCount; i++ {
		generationID := buildGenerationID(req, i)
		item, genErr := generateWithRetry(ctx, u.barcode, u.reconciler, req, engineFields, generationID)
		if genErr != nil {
			failedCount++
			continue
		}
		item.GenerationID = generationID
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

	// ПЛАН §3.3: accepted render checkpoint (stable generationId + barcodes)
	// сохраняем ДО Capture, чтобы status reconciliation видел уже отрендеренные
	// баркоды даже при сбое финализации.
	u.saveRenderCheckpoint(ctx, req, barcodes)

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

	// Compute total cost for successful units (п.12 ТЗ).
	//
	// Основной путь: unitPrice возвращает Billing — BFF использует его напрямую (п.1 ТЗ: BFF = orchestrator).
	// Fallback: unitPrice отсутствует (<=0) — вычисляем пропорционально фактически списанным суммам
	// по ВСЕМ источникам (Subscription.Amount + Credits.Amount + Wallet.Amount) относительно
	// AllowedTotal. Знаменатель = AllowedTotal (не wallet.Units!), иначе при Split Payment waterfall
	// (Subscription→Credits→Wallet, п.6 ТЗ) стоимость завышается: юниты подписки и кредитов
	// «прячутся» в wallet.Units и цена на юнит удваивается.
	walletAmount := quote.BySource.Wallet.Amount
	var totalCost float64
	if quote.UnitPrice > 0 {
		totalCost = float64(successCount) * quote.UnitPrice
	} else {
		// Fallback: суммируем денежные суммы по всем источникам и масштабируем на successCount.
		totalAmount := quote.BySource.Subscription.Amount +
			quote.BySource.Credits.Amount +
			walletAmount
		totalCost = float64(successCount) * totalAmount / float64(max(quote.AllowedTotal, 1))
	}

	billing := domain.GenerateBillingResult{
		TotalCost:        totalCost,
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
			UserID:       userID,
			BuildID:      req.BuildID,
			BatchID:      req.BatchID,
			Revision:     req.Revision,
			BarcodeType:  req.BarcodeType,
			BarcodeURL:   bc.URL,
			GenerationID: bc.GenerationID,
			Fields:       resolvedFields,
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
	reconciler ports.RenderReconciler,
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
		log.Printf("generate: barcodegen attempt %d/%d failed (revision=%s, type=%s): %v", attempt+1, maxBarcodeGenRetries, req.Revision, req.BarcodeType, err)
		// ПЛАН §B.5: ambiguous timeout/сетевую ошибку разрешаем через reconciliation
		// внутреннего render registry — повторный энкодер не запускаем.
		if reconciler != nil && idempotencyKey != "" {
			if res, rerr := reconciler.RenderStatus(ctx, idempotencyKey); rerr == nil {
				switch strings.ToUpper(res.Status) {
				case "SUCCEEDED":
					metrics.BarcodeGenCallsTotal.WithLabelValues("reconciled").Inc()
					return domain.BarcodeItem{URL: res.BarcodeURL, Format: res.Format, GenerationID: idempotencyKey}, nil
				case "FAILED":
					metrics.BarcodeGenCallsTotal.WithLabelValues("error").Inc()
					return domain.BarcodeItem{}, domain.NewBarcodeGenError(
						fmt.Errorf("render %s failed: %s", idempotencyKey, res.ErrorCategory))
				}
			}
		}
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

// cloneFields — поверхностная копия входных полей: choice fallbacks и
// public→engine маппинг не должны мутировать карту вызывающего.
func cloneFields(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// digitsOnly оставляет только цифры (принимаем MMDDYYYY / MM/DD/YYYY / MM-DD-YYYY).
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var (
	dateISORe = regexp.MustCompile(`^(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})$`)
	dateUSRe  = regexp.MustCompile(`^(\d{1,2})[-/.](\d{1,2})[-/.](\d{4})$`)
)

// parseDMY разбирает дату из YYYY-MM-DD / MM/DD/YYYY / MMDDYYYY (и вариаций разделителей).
func parseDMY(s string) (mm, dd, yyyy int, ok bool) {
	s = strings.TrimSpace(s)
	if m := dateISORe.FindStringSubmatch(s); m != nil {
		yyyy, _ = strconv.Atoi(m[1])
		mm, _ = strconv.Atoi(m[2])
		dd, _ = strconv.Atoi(m[3])
	} else if m := dateUSRe.FindStringSubmatch(s); m != nil {
		mm, _ = strconv.Atoi(m[1])
		dd, _ = strconv.Atoi(m[2])
		yyyy, _ = strconv.Atoi(m[3])
	} else {
		d := digitsOnly(s)
		if len(d) != 8 {
			return 0, 0, 0, false
		}
		mm, _ = strconv.Atoi(d[0:2])
		dd, _ = strconv.Atoi(d[2:4])
		yyyy, _ = strconv.Atoi(d[4:8])
	}
	if mm < 1 || mm > 12 || dd < 1 || dd > 31 || yyyy < 1900 || yyyy > 2100 {
		return 0, 0, 0, false
	}
	return mm, dd, yyyy, true
}

// applyRenderTransforms приводит engine-поля к формату exact-профиля перед render:
//   - даты DBB/DBD/DBA: US → MMDDYYYY, CA → YYYYMMDD;
//   - DBB младше 16 лет → VALIDATION_ERROR (защита getRandomDate от зацикливания);
//   - DAU: дюймы → см (ON/AB).
func applyRenderTransforms(fields map[string]any, cfg domain.RevisionConfig) *domain.AppError {
	for _, code := range []string{"DBB", "DBD", "DBA"} {
		raw, ok := fields[code]
		if !ok || raw == nil {
			continue
		}
		s, _ := raw.(string)
		if s == "" {
			continue
		}
		mm, dd, yyyy, ok := parseDMY(s)
		if !ok {
			return domain.NewValidationError("invalid date value for " + code)
		}
		us := fmt.Sprintf("%02d%02d%04d", mm, dd, yyyy)
		if code == "DBB" {
			now := time.Now()
			age := now.Year() - yyyy
			if now.Month() < time.Month(mm) || (now.Month() == time.Month(mm) && now.Day() < dd) {
				age--
			}
			if age < 16 {
				return domain.NewValidationError("date of birth must be at least 16 years ago")
			}
		}
		if cfg.Country == "CA" {
			fields[code] = fmt.Sprintf("%04d%02d%02d", yyyy, mm, dd)
		} else {
			fields[code] = us
		}
	}
	if cfg.HeightCm {
		if raw, ok := fields["DAU"]; ok && raw != nil {
			if n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", raw))); err == nil {
				fields["DAU"] = strconv.Itoa(int(math.Round(float64(n) * 2.54)))
			}
		}
	}
	// Georgia: county → ZGD (нормализация + подтверждённые source-исключения).
	if daj, _ := fields["DAJ"].(string); daj == "GA" {
		if z, ok := fields["ZGD"].(string); ok {
			up := strings.ToUpper(strings.TrimSpace(z))
			switch up {
			case "SCHLEY":
				up = "SCHELEY"
			case "TREUTLEN":
				up = "TRETLEN"
			}
			fields["ZGD"] = up
		}
	}
	return nil
}

// buildGenerationID возвращает stable generationId юнита (ПЛАН §3.3). Он же
// используется как renderKey BarcodeGen: retry/replay при потерянном кэше
// идемпотентности переиспользует тот же barcode, а не создаёт дубль.
func buildGenerationID(req domain.GenerateRequest, index int) string {
	if req.IdempotencyKey == "" {
		return ""
	}
	return domain.StableGenerationID(req.IdempotencyKey, index)
}

// generateRequestHash — детерминированный отпечаток тела generate-запроса,
// защищающий accepted checkpoints от повторного использования с другим телом.
func generateRequestHash(req domain.GenerateRequest) string {
	payload := struct {
		Revision          string         `json:"revision"`
		Mode              string         `json:"mode"`
		Units             int            `json:"units"`
		BuildID           string         `json:"buildId"`
		BatchID           string         `json:"batchId"`
		Confirmed         bool           `json:"confirmed"`
		GenerateSignature bool           `json:"generateSignature"`
		SignatureStyle    string         `json:"signatureStyle"`
		GeneratePhoto     bool           `json:"generatePhoto"`
		PhotoDescription  string         `json:"photoDescription"`
		Gender            string         `json:"gender"`
		Age               int            `json:"age"`
		Fields            map[string]any `json:"fields"`
	}{
		Revision:          req.Revision,
		Mode:              req.Mode,
		Units:             req.Units,
		BuildID:           req.BuildID,
		BatchID:           req.BatchID,
		Confirmed:         req.Confirmed,
		GenerateSignature: req.GenerateSignature,
		SignatureStyle:    req.SignatureStyle,
		GeneratePhoto:     req.GeneratePhoto,
		PhotoDescription:  req.PhotoDescription,
		Gender:            req.Gender,
		Age:               req.Age,
		Fields:            req.Fields,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// loadDeriveCheckpoint возвращает accepted derive checkpoint, только если он
// принадлежит тому же телу запроса (совпадение request hash).
func (u *GenerateUseCase) loadDeriveCheckpoint(ctx context.Context, req domain.GenerateRequest) (deriveCheckpoint, bool) {
	if u.idem == nil || req.IdempotencyKey == "" {
		return deriveCheckpoint{}, false
	}
	raw, found, err := u.idem.GetCheckpoint(ctx, req.IdempotencyKey, deriveCheckpointName)
	if err != nil || !found {
		return deriveCheckpoint{}, false
	}
	var cp deriveCheckpoint
	if json.Unmarshal(raw, &cp) != nil || cp.Fields == nil {
		return deriveCheckpoint{}, false
	}
	if cp.RequestHash == "" || cp.RequestHash != generateRequestHash(req) {
		return deriveCheckpoint{}, false
	}
	return cp, true
}

// saveDeriveCheckpoint сохраняет принятый derive-результат (best-effort).
func (u *GenerateUseCase) saveDeriveCheckpoint(ctx context.Context, req domain.GenerateRequest, fields map[string]any, computed, skipped []string) {
	if u.idem == nil || req.IdempotencyKey == "" {
		return
	}
	raw, err := json.Marshal(deriveCheckpoint{
		RequestHash: generateRequestHash(req),
		Fields:      fields,
		Computed:    computed,
		Skipped:     skipped,
	})
	if err != nil {
		return
	}
	_ = u.idem.SetCheckpoint(ctx, req.IdempotencyKey, deriveCheckpointName, raw)
}

// saveRenderCheckpoint сохраняет принятый render-результат (best-effort).
func (u *GenerateUseCase) saveRenderCheckpoint(ctx context.Context, req domain.GenerateRequest, barcodes []domain.BarcodeItem) {
	if u.idem == nil || req.IdempotencyKey == "" || len(barcodes) == 0 {
		return
	}
	ids := make([]string, 0, len(barcodes))
	for _, b := range barcodes {
		ids = append(ids, b.GenerationID)
	}
	raw, err := json.Marshal(renderCheckpoint{GenerationIDs: ids, Barcodes: barcodes})
	if err != nil {
		return
	}
	_ = u.idem.SetCheckpoint(ctx, req.IdempotencyKey, renderCheckpointName, raw)
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

// missingRenderFields возвращает отсутствующие обязательные render-поля профиля.
func missingRenderFields(required []string, fields map[string]any) []string {
	var missing []string
	for _, f := range required {
		if !hasUserValue(fields, f) {
			missing = append(missing, f)
		}
	}
	return missing
}

// validatePreparedDates проверяет готовую пару [DBD,DBA] в prepared-режиме без
// перегенерации: malformed/month 00 → 422 INVALID_GENERATED_DATE, раньше ревизии →
// 422 ISSUE_DATE_BEFORE_REVISION. Пропускается, если даты не заданы или профиль
// не объявляет revisionEffectiveDate.
func validatePreparedDates(fields map[string]any, effectiveDate string) *domain.AppError {
	dbd, _ := fields["DBD"].(string)
	dba, _ := fields["DBA"].(string)
	if dbd == "" || dba == "" || effectiveDate == "" {
		return nil
	}
	if err := ValidateGeneratedDates(dbd, dba, effectiveDate, RealClock{}); err != nil {
		kind := "malformed"
		var de *DateValidationError
		if errors.As(err, &de) && de.Kind == DateBeforeRevision {
			kind = "before_revision"
		}
		return domain.NewInvalidGeneratedDateError(kind, err.Error())
	}
	return nil
}

// filterRenderValues оставляет только allowlisted engine-поля профиля. Пустой
// allowlist — профиль пока не ограничивает набор (permissive, обратная совместимость).
func filterRenderValues(allowlist []string, fields map[string]any) map[string]any {
	if len(allowlist) == 0 {
		return fields
	}
	allowed := make(map[string]bool, len(allowlist))
	for _, f := range allowlist {
		allowed[f] = true
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		if allowed[k] {
			out[k] = v
		}
	}
	return out
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
