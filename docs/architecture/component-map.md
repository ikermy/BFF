# Component Map

## API Process (`cmd/api`)

- Gin transport: `internal/transport/http/gin`
- Use cases: `internal/usecase`
- Ports: `internal/ports`
- Adapters: `internal/adapters`

## Worker Process (`cmd/worker`)

- Kafka transport: `internal/transport/kafka`
- Bulk processing: `internal/transport/kafka/bulk_job_handler.go` + `internal/usecase/generate_usecase.go`

## Flow Boundaries

- `/api/v1/*` — frontend client API
- `/internal/*` — service API для Bulk Service и внутренних сервисов
- `/admin/*` — административный API

---

## BFF Dependency Map (п.3.3 ТЗ)

```
                        ┌─────────────────────────┐
                        │          BFF             │
                        │  (Backend for Frontend)  │
                        └──────────┬──────────────┘
                                   │
          ┌──────────┬─────────────┼────────────┬──────────────┬────────────────┐
          ▼          ▼             ▼            ▼              ▼                ▼
     ┌─────────┐ ┌──────────┐ ┌────────┐ ┌─────────┐ ┌──────────────┐ ┌────────────────┐
     │ Billing │ │BarcodeGen│ │AI Svc  │ │ History │ │Auth (Legacy) │ │Notif (Legacy)  │
     └─────────┘ └──────────┘ └────────┘ └─────────┘ └──────────────┘ └────────────────┘

     + TransHistory (Legacy) — Kafka топик trans-history.log (п.11.3 ТЗ)
```

### Go-порты (интерфейсы) по сервисам

| Сервис            | Порт Go                  | Пакет-адаптер               | Категория      |
|-------------------|--------------------------|-----------------------------|----------------|
| Billing           | `BillingClient`          | `adapters/billing`          | Core           |
| BarcodeGen        | `BarcodeGenClient`       | `adapters/barcodegen`       | Core           |
| AI Service        | `AIClient`               | `adapters/ai`               | Core           |
| History           | `HistoryClient`          | `adapters/history`          | Core           |
| Auth              | `AuthClient`             | `adapters/auth`             | Legacy Bridge  |
| Notifications     | `NotificationsPublisher` | `adapters/events`           | Legacy Bridge  |
| TransHistory      | `TransHistoryPublisher`  | `adapters/events`           | Legacy Bridge  |

> **Примечание:** `TransHistoryPublisher` отсутствует в оригинальной схеме п.3.3 ТЗ,
> но реализован в коде (п.11.3 ТЗ). Относится к Legacy Bridges.

### NestJS → Go: соответствие модулей

| NestJS-модуль (`src/modules/`)  | Go Clean Architecture               |
|---------------------------------|-------------------------------------|
| `barcode/barcode.controller.ts` | `transport/http/gin/api_handler.go` |
| `barcode/barcode.service.ts`    | `usecase/generate_usecase.go`       |
| `barcode/chain-executor.service.ts` | `usecase/chain_executor.go`     |
| `billing/billing.client.ts`     | `ports.BillingClient` + `adapters/billing` |
| `barcodegen/barcodegen.client.ts` | `ports.BarcodeGenClient` + `adapters/barcodegen` |
| `auth/auth.client.ts`           | `ports.AuthClient` + `adapters/auth` |
| `notifications/notifications.client.ts` | `ports.NotificationsPublisher` + `adapters/events` |
| `trans-history/trans-history.client.ts` | `ports.TransHistoryPublisher` + `adapters/events` |
| `config/revisions/`             | `adapters/revisions` + `configs/revisions/*.yaml` |
