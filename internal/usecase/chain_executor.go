package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/metrics"
	"github.com/ikermy/BFF/internal/ports"
)

// ChainExecutor — выполняет цепочку генерации полей для ревизии (п.4 ТЗ).
//
// КЛЮЧЕВОЕ ПРАВИЛО (п.4.2 ТЗ): Пользовательский ввод НИКОГДА не перезаписывается!
// Если пользователь заполнил поле — шаг пропускается, поле попадает в Skipped.
type ChainExecutor struct {
	barcodeGen ports.BarcodeGenClient
	revisions  ports.RevisionConfigStore
}

func NewChainExecutor(barcodeGen ports.BarcodeGenClient, revisions ports.RevisionConfigStore) *ChainExecutor {
	return &ChainExecutor{barcodeGen: barcodeGen, revisions: revisions}
}

// Execute выполняет цепочку вычислений для заданной ревизии.
//
// Алгоритм (п.4.2 ТЗ):
//  1. Копируем userInput в resolvedFields.
//  2. Для каждого шага цепочки:
//     a. Если поле уже заполнено пользователем → Skipped (не трогаем!).
//     b. Проверяем dependsOn — все зависимости должны быть в resolvedFields.
//     c. Вызываем BarcodeGen.Calculate или .Random в зависимости от source.
//     d. Сохраняем результат в resolvedFields.
func (e *ChainExecutor) Execute(ctx context.Context, revision string, userInput map[string]any) (domain.ChainResult, error) {
	start := time.Now()
	defer func() {
		metrics.ChainExecutionDurationMs.Observe(float64(time.Since(start).Milliseconds()))
	}()

	cfg, err := e.revisions.GetConfig(ctx, revision)
	if err != nil {
		return domain.ChainResult{}, domain.NewValidationError("revision not found: " + revision)
	}

	// Начинаем с копии пользовательского ввода
	resolvedFields := make(map[string]any, len(userInput))
	for k, v := range userInput {
		resolvedFields[k] = v
	}

	computed := make([]string, 0, len(cfg.CalculationChain))
	var skipped []string

	for _, step := range cfg.CalculationChain {
		// КЛЮЧЕВАЯ ПРОВЕРКА: если пользователь заполнил — пропускаем! (п.4.2 ТЗ)
		if hasUserValue(userInput, step.Field) {
			skipped = append(skipped, step.Field)
			continue
		}

		// Проверяем что все зависимости уже присутствуют в resolvedFields (п.5.2 ТЗ)
		if len(step.DependsOn) > 0 {
			var missing []string
			for _, dep := range step.DependsOn {
				if !hasUserValue(resolvedFields, dep) {
					missing = append(missing, dep)
				}
			}
			if len(missing) > 0 {
				return domain.ChainResult{}, domain.NewMissingDependencyError(step.Field, missing)
			}
		}

		var value any
		switch step.Source {
		case "calculate":
			value, err = e.barcodeGen.Calculate(ctx, revision, step.Field, resolvedFields)
			if err != nil {
				return domain.ChainResult{}, domain.NewBarcodeGenError(err)
			}
		case "random":
			value, err = e.barcodeGen.Random(ctx, revision, step.Field, step.Params)
			if err != nil {
				return domain.ChainResult{}, domain.NewBarcodeGenError(err)
			}
		default:
			// source=user: поле должно быть заполнено пользователем, пропускаем
			skipped = append(skipped, step.Field)
			continue
		}

		resolvedFields[step.Field] = value
		computed = append(computed, step.Field)
	}

	return domain.ChainResult{
		Fields:   resolvedFields,
		Computed: computed,
		Skipped:  skipped,
	}, nil
}

// hasUserValue проверяет, заполнил ли пользователь поле (п.4.2 ТЗ).
// Пустая строка, nil — считаются НЕ заполненными.
func hasUserValue(input map[string]any, field string) bool {
	value, ok := input[field]
	if !ok || value == nil {
		return false
	}
	if str, isStr := value.(string); isStr && strings.TrimSpace(str) == "" {
		return false
	}
	return true
}
