package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
)

// EditUseCase — оркестрирует бесплатное редактирование баркода (п.10.1 ТЗ).
//
// Flow:
//  1. CheckFreeEdit → History Service: проверяем editFlag
//  2. Если editFlag=true → 402 (право уже использовано)
//  3. Block {units:0} → Billing (бесплатная блокировка)
//  4. Generate → BarcodeGen (перегенерация с новым значением поля)
//  5. PublishBarcodeEdited → Kafka: Consumer ставит editFlag=true и обновляет imageUrl
type EditUseCase struct {
	billing    ports.BillingClient
	barcodeGen ports.BarcodeGenClient
	history    ports.HistoryClient
	events     ports.EventPublisher
}

func NewEditUseCase(
	billing ports.BillingClient,
	barcodeGen ports.BarcodeGenClient,
	history ports.HistoryClient,
	events ports.EventPublisher,
) *EditUseCase {
	return &EditUseCase{
		billing:    billing,
		barcodeGen: barcodeGen,
		history:    history,
		events:     events,
	}
}

// Execute — выполняет бесплатное редактирование баркода.
// userID нужен для billing.Block (units=0 — бесплатная блокировка, только регистрация саги).
func (u *EditUseCase) Execute(ctx context.Context, userID, barcodeID string, req domain.EditRequest) (domain.EditResponse, error) {
	if barcodeID == "" {
		return domain.EditResponse{}, domain.NewValidationError("barcodeId is required")
	}
	if req.Field == "" {
		return domain.EditResponse{}, domain.NewValidationError("field is required")
	}

	// Шаг 1: проверить право на бесплатное редактирование
	canEdit, err := u.history.CheckFreeEdit(ctx, barcodeID)
	if err != nil {
		return domain.EditResponse{}, domain.NewBillingError(err)
	}
	if !canEdit {
		return domain.EditResponse{
				CanEdit: false,
				Reason:  "edit_already_used",
			}, &domain.AppError{
				Code:       domain.ErrCodeInsufficientFunds,
				HTTPStatus: 402,
				Message:    "free edit right already used for barcode " + barcodeID,
			}
	}

	record, err := u.history.GetBarcode(ctx, barcodeID)
	if err != nil {
		return domain.EditResponse{}, domain.NewBarcodeGenError(err)
	}

	// Шаг 3: бесплатная блокировка (units=0) — Billing регистрирует сагу без списания (п.10.1 ТЗ).
	// BySource намеренно нулевой: "POST /block {credits: 0}" из ТЗ.
	// ВАЖНО: ошибку Block нельзя игнорировать. Если Block упал — Billing не знает о саге,
	// но barcode.edited всё равно улетит в Kafka → History поставит editFlag=true.
	// Итог: split-brain (сага есть в History, нет в Billing) + editFlag сгорел впустую.
	sagaID := "edit-" + barcodeID
	if err := u.billing.Block(ctx, domain.BlockRequest{
		UserID: userID,
		Units:  0,
		SagaID: sagaID,
	}); err != nil {
		return domain.EditResponse{}, domain.NewBillingError(err)
	}

	// Шаг 4: генерация нового баркода с обновлённым полем поверх исходных полей
	fields := make(map[string]any, len(record.Fields)+1)
	for k, v := range record.Fields {
		fields[k] = v
	}
	fields[req.Field] = req.Value

	var newURL string
	switch strings.ToLower(record.BarcodeType) {
	case "", "pdf417":
		resp, genErr := u.barcodeGen.GeneratePDF417(ctx, domain.GeneratePDF417Request{
			Revision:       record.Revision,
			Fields:         fields,
			IdempotencyKey: req.IdempotencyKey,
		})
		if genErr != nil {
			return domain.EditResponse{}, domain.NewBarcodeGenError(genErr)
		}
		newURL = resp.BarcodeURL
	case "code128":
		data, _ := fields["data"].(string)
		if strings.TrimSpace(data) == "" {
			return domain.EditResponse{}, domain.NewValidationError("data field is required for code128 edit")
		}
		resp, genErr := u.barcodeGen.GenerateCode128(ctx, domain.GenerateCode128Request{
			Data:           data,
			IdempotencyKey: req.IdempotencyKey,
		})
		if genErr != nil {
			return domain.EditResponse{}, domain.NewBarcodeGenError(genErr)
		}
		newURL = resp.BarcodeURL
	default:
		return domain.EditResponse{}, domain.NewValidationError("unsupported barcodeType: " + record.BarcodeType)
	}

	// Шаг 5: публикация события — Consumer обновит editFlag=true и imageUrl (п.10.1 ТЗ).
	// Критично: History Service ожидает barcode.edited для установки editFlag.
	// Без этого события editFlag останется false → пользователь сможет редактировать повторно бесплатно.
	// Возвращаем ошибку — клиент должен повторить запрос.
	if err := u.events.PublishBarcodeEdited(ctx, domain.BarcodeEditedEvent{
		ID:        barcodeID,
		Field:     req.Field,
		NewURL:    newURL,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return domain.EditResponse{}, &domain.AppError{
			Code:       domain.ErrCodeBarcodeGenError,
			HTTPStatus: 503,
			Message:    "failed to publish barcode.edited event: " + err.Error(),
		}
	}

	return domain.EditResponse{
		NewURL:  newURL,
		CanEdit: true,
	}, nil
}
