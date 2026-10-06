package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"
)

// ChainExecutor — выполняет цепочку генерации полей для ревизии (п.4 ТЗ).
//
// КЛЮЧЕВОЕ ПРАВИЛО (п.4.2 ТЗ): Пользовательский ввод НИКОГДА не перезаписывается!
// Если пользователь заполнил поле — шаг пропускается, поле попадает в Skipped.
type ChainExecutor struct {
	barcodeGen   ports.BarcodeGenClient
	revisions    ports.RevisionConfigStore
	deriver      ports.BarcodeFieldDeriver // grouped derive (ПЛАН §3.3); nil → per-field fallback
	clock        Clock
	dateAttempts int
}

func NewChainExecutor(barcodeGen ports.BarcodeGenClient, revisions ports.RevisionConfigStore) *ChainExecutor {
	return &ChainExecutor{barcodeGen: barcodeGen, revisions: revisions, dateAttempts: 20}
}

// WithDeriver подключает grouped derive-порт (для GenerationSteps).
func (e *ChainExecutor) WithDeriver(d ports.BarcodeFieldDeriver) *ChainExecutor {
	e.deriver = d
	return e
}

// WithClock подключает часы для date-валидатора.
func (e *ChainExecutor) WithClock(c Clock) *ChainExecutor {
	e.clock = c
	return e
}

// WithDateAttempts задаёт лимит повторов date-step.
func (e *ChainExecutor) WithDateAttempts(n int) *ChainExecutor {
	if n > 0 {
		e.dateAttempts = n
	}
	return e
}

// ExecuteGrouped выполняет grouped GenerationSteps профиля (ПЛАН §3.3) — это
// единственная runtime-модель генерации. Legacy CalculationChain больше не
// исполняется. Профиль без GenerationSteps (например LA, где поля задаёт
// пользователь) просто возвращает вход без вычислений.
//
// Правила: пользовательские значения не перезаписываются; date-step (output
// содержит DBD и DBA) проходит bounded-повтор через ValidateGeneratedDates;
// отсутствие deriver'а → per-field fallback через BarcodeGenClient.
func (e *ChainExecutor) ExecuteGrouped(ctx context.Context, revision string, baseInput map[string]any) (domain.ChainResult, error) {
	cfg, err := e.revisions.GetConfig(ctx, revision)
	if err != nil {
		return domain.ChainResult{}, domain.NewValidationError("revision not found: " + revision)
	}

	resolved := make(map[string]any, len(baseInput))
	for k, v := range baseInput {
		resolved[k] = v
	}
	// public→engine алиасы (dateOfBirth→DBB, state→DAJ, …) до выполнения шагов:
	// step.Input оперирует engine-кодами, а форма присылает public-имена.
	resolved = domain.NormalizeEngineFields(resolved)
	// static-константы профиля доступны и derive-шагам (напр. DDA для CO/AZ).
	domain.ApplyEngineDefaults(resolved, cfg.Defaults)

	if len(cfg.GenerationSteps) == 0 {
		return domain.ChainResult{Fields: resolved}, nil
	}

	computed := make([]string, 0)
	skipped := make([]string, 0)

	for _, step := range cfg.GenerationSteps {
		input := pickFields(resolved, step.Input)

		outputs, err := e.runStep(ctx, revision, cfg, step, input)
		if err != nil {
			return domain.ChainResult{}, err
		}

		for _, field := range step.Output {
			if hasUserValue(baseInput, field) {
				skipped = append(skipped, field)
				continue
			}
			value, ok := outputs[field]
			if !ok || value == nil {
				return domain.ChainResult{}, domain.NewValidationError("step " + step.ID + " produced no value for " + field)
			}
			resolved[field] = value
			computed = append(computed, field)
		}
	}

	return domain.ChainResult{Fields: resolved, Computed: computed, Skipped: skipped}, nil
}

// runStep выполняет один grouped-шаг; для date-step применяет валидатор дат.
func (e *ChainExecutor) runStep(ctx context.Context, revision string, cfg domain.RevisionConfig, step domain.GenerationStep, input map[string]any) (map[string]any, error) {
	if isDateStep(step.Output) {
		clk := e.clock
		if clk == nil {
			clk = RealClock{}
		}
		dbd, dba, err := FetchValidatedDateStep(e.dateAttempts, func() (string, string, error) {
			out, derr := e.derive(ctx, revision, step.Endpoint, input, step.Output)
			if derr != nil {
				return "", "", derr
			}
			return fmt.Sprint(out["DBD"]), fmt.Sprint(out["DBA"]), nil
		}, cfg.RevisionEffectiveDate, clk)
		if err != nil {
			var de *DateValidationError
			if asDateError(err, &de) {
				return nil, domain.NewBarcodeGenInvalidDateError(err)
			}
			return nil, domain.NewBarcodeGenError(err)
		}
		out := make(map[string]any, len(step.Output))
		for _, f := range step.Output {
			switch f {
			case "DBD":
				out[f] = dbd
			case "DBA":
				out[f] = dba
			}
		}
		return out, nil
	}

	out, err := e.derive(ctx, revision, step.Endpoint, input, step.Output)
	if err != nil {
		return nil, domain.NewBarcodeGenError(err)
	}
	return out, nil
}

// derive вызывает grouped deriver, либо per-field fallback через BarcodeGenClient.
func (e *ChainExecutor) derive(ctx context.Context, revision, endpoint string, input map[string]any, output []string) (map[string]any, error) {
	if e.deriver != nil {
		return e.deriver.Derive(ctx, revision, endpoint, input, output)
	}
	out := make(map[string]any, len(output))
	for _, field := range output {
		var (
			value any
			err   error
		)
		switch endpoint {
		case "random":
			value, err = e.barcodeGen.Random(ctx, revision, field, input)
		case "calculate":
			value, err = e.barcodeGen.Calculate(ctx, revision, field, input)
		default:
			return nil, fmt.Errorf("unsupported derive endpoint: %s", endpoint)
		}
		if err != nil {
			return nil, err
		}
		out[field] = value
	}
	return out, nil
}

// asDateError — errors.As для *DateValidationError без импорта errors в вызывающем.
func asDateError(err error, target **DateValidationError) bool {
	for err != nil {
		if de, ok := err.(*DateValidationError); ok {
			*target = de
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

func isDateStep(outputs []string) bool {
	var hasDBD, hasDBA bool
	for _, o := range outputs {
		if o == "DBD" {
			hasDBD = true
		}
		if o == "DBA" {
			hasDBA = true
		}
	}
	return hasDBD && hasDBA
}

// pickFields выбирает указанные поля из resolved, если они заданы.
func pickFields(resolved map[string]any, fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		if v, ok := resolved[f]; ok {
			out[f] = v
		}
	}
	return out
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
