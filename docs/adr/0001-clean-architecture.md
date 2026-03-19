# ADR 0001: Clean Architecture

- Статус: accepted
- Дата: 2026-03-17

## Решение

Используем слои:

- `internal/domain` — доменные структуры
- `internal/usecase` — сценарии
- `internal/ports` — интерфейсы внешнего мира
- `internal/adapters` — реализации интерфейсов
- `internal/transport` — HTTP/Kafka transport

Зависимости направлены внутрь, транспорт и адаптеры зависят от use-case/ports.

---

## Принцип BFF — ТОЛЬКО оркестратор (п.1.2 ТЗ)

**КРИТИЧЕСКИ ВАЖНО:** BFF НЕ содержит бизнес-логики расчётов.
Вся логика вычислений, рандомизации и генерации полей находится в BarcodeGen.

### ✅ BFF делает (Orchestration)

| Ответственность        | Где реализовано                          |
|------------------------|------------------------------------------|
| Валидация входных данных | `usecase/*_usecase.go`, `validateMinimumSet` (п.5.2) |
| Определение порядка вызовов | `GenerateUseCase.Execute`: quote → block → chain → generate → capture/release |
| Вызов BarcodeGen.Calculate | `usecase/chain_executor.go` |
| Вызов BarcodeGen.Random    | `usecase/chain_executor.go` |
| Проверка и блокировка оплаты | `QuoteUseCase`, `billing.Block/Capture/Release` |
| Сборка финального результата | `domain.GenerateResponse` в `GenerateUseCase` |

### ❌ BFF не делает (Domain — всё в BarcodeGen)

- Расчёт значений полей
- Генерация случайных данных
- Бизнес-логика ревизий
- Форматирование полей
- Кодирование баркода
- Тяжёлые математические вычисления

---

## User Stories (п.2.1 ТЗ)

| ID   | User Story                              | Реализация                                   |
|------|-----------------------------------------|----------------------------------------------|
| US-1 | Узнать сколько баркодов можно сгенерировать | `QuoteUseCase` + `GET /api/v1/billing/quote` |
| US-2 | Partial success — генерировать сколько возможно | `PARTIAL_FUNDS` + Compensating Transactions (`generate_usecase.go`) |
| US-3 | Вызывать BarcodeGen для расчётов (без дублирования логики) | `ChainExecutor` — делегирует `.Calculate`/`.Random` |
| US-4 | Сохранять пользовательский ввод (не перезаписывать) | `hasUserValue` в `chain_executor.go` |
| US-5 | Настраивать конфиги через API (без деплоя) | `AdminHandler` + `PUT /admin/revisions/{revision}` |
