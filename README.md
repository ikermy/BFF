# Barcode BFF Service

`Barcode BFF` — Backend-For-Frontend оркестратор платформы Barcode Site.

## Repository / Go module

- GitHub repository: `github.com/ikermy/BFF`
- Go module: `github.com/ikermy/BFF`

Если проект используется как зависимость или клонируется в новое окружение, ориентируйтесь именно на этот module path.

### Quick start from scratch

#### Clone repository

```bash
git clone https://github.com/ikermy/BFF.git
cd BFF
```

#### Install dependencies

```bash
go mod tidy
```

#### First local run

```bash
cp .env.example .env
go run ./cmd/api
```

Worker запускается во втором терминале:

```bash
go run ./cmd/worker
```

Ключевой принцип проекта согласно ТЗ:

> **BFF НЕ содержит бизнес-логики расчётов.**  
> Он управляет потоком данных, вызывает внешние сервисы в правильном порядке,
> валидирует минимальный набор входных данных, оркестрирует оплату,
> генерацию баркодов, AI, History и публикацию событий в Kafka.

---

## 1. Что делает сервис

Сервис отвечает за следующие сценарии:

- проверка доступных средств через Billing;
- частичная/полная генерация баркодов;
- выполнение `calculationChain` по ревизии;
- free edit для уже сгенерированного баркода;
- remake через повторную генерацию;
- bulk-обработка задач через Kafka topic `bulk.tasks`;
- публикация событий в Kafka (`barcode.generated`, `barcode.edited`, `billing.saga.partial_completed` и др.);
- интеграции с legacy-мостами: `Auth`, `Notifications`, `TransHistory`.

---

## 2. Архитектура проекта

### Структура каталогов

```text
.
├── .env.example
├── Makefile
├── Dockerfile
├── docker-compose.yml
├── docker-compose.dev.yml
├── docker-compose.test.yml
├── docker-compose.prod.yml
├── docker-compose.observability.yml
├── cmd/
│   ├── api/        → HTTP API процесс
│   └── worker/     → Kafka worker процесс
├── configs/
│   ├── admin/      → file-backed admin-конфиги (`timeouts.yaml`, `topup-bonus.yaml`)
│   └── revisions/  → YAML-конфиги ревизий
├── deploy/
├── docs/
│   ├── adr/
│   ├── architecture/
│   ├── operations/
│   └── openapi.yaml
├── internal/
│   ├── adapters/   → HTTP / Kafka / Redis / in-memory реализации портов
│   ├── app/        → DI и сборка приложения
│   ├── config/     → ENV-конфигурация
│   ├── domain/     → доменные модели и ошибки
│   ├── metrics/    → Prometheus метрики
│   ├── ports/      → интерфейсы внешних зависимостей
│   ├── transport/  → Gin handlers и Kafka handlers
│   └── usecase/    → orchestration / application logic
└── monitoring/
    ├── prometheus.yml
    ├── loki-config.yml
    ├── promtail-config.yml
    └── grafana/
```

### Процессы

- `cmd/api` — HTTP API на Gin.
- `cmd/worker` — Kafka consumer для `bulk.tasks`.

### Границы API

- `/api/v1/*` — клиентский API
- `/admin/*` — административный API
- `/internal/*` — service-to-service API
- `/health` — health endpoint
- `/metrics` — Prometheus metrics

Дополнительная архитектурная карта:
- `docs/architecture/component-map.md`

---

## 3. Ключевые паттерны реализации

### Chain Executor

Выполняет `calculationChain` из ревизии.

Правила:
- пользовательский ввод **никогда не перезаписывается**;
- `dependsOn` обязательны;
- `calculate` и `random` выполняются через BarcodeGen;
- отсутствие зависимости приводит к `MISSING_DEPENDENCY`.

### Partial Success

Если удалось сгенерировать только часть баркодов:

- `Capture(successCount)`
- `Release(failedCount)`
- publish `billing.saga.partial_completed`

### Idempotency

Для write-операций используется `X-Idempotency-Key`.

Алгоритм:
- `Reserve`
- выполнить запрос
- сохранить response
- при повторе вернуть cached response с кодом `DUPLICATE_REQUEST` (`HTTP 200`)

### Retry

Для BarcodeGen:

- `MAX_RETRIES = 3`
- задержки: `1s`, `3s`, `5s`
- `5xx` и сетевые ошибки — retryable
- `4xx` — не retryable

### Free Edit

Один баркод имеет право на одно бесплатное редактирование поля:

- проверка через History (`editFlag`)
- регенерация баркода
- publish `barcode.edited`

---

## 4. Основные зависимости

### Core

- Billing
- BarcodeGen
- AI Service
- History

### Legacy bridges

- Auth (HTTP)
- Notifications (Kafka)
- TransHistory (Kafka)

### Infra

- Kafka
- Redis
- Prometheus
- Loki
- Promtail
- Grafana

---

## 5. Предварительные требования

### Для локального запуска без Docker

Нужно установить:

- Go `1.25+`
- Git

### Для запуска в Docker

Нужно установить:

- Docker
- Docker Compose v2

### Для запуска через Makefile

Нужно установить:

- `make`

На Windows это обычно:

- Git Bash / MSYS2 / WSL
- либо запускать напрямую команды `docker compose` без `make`

На Linux/macOS обычно достаточно штатного `make` из пакетов системы.

---

## 6. Конфигурация

Все основные параметры задаются через ENV.

Пример файла:
- `.env.example`

Скопируйте его в `.env` и отредактируйте при необходимости.

### Обязательные/важные переменные

#### Базовые

- `BFF_PORT`
- `INTERNAL_SERVICE_JWT`
- `ADMIN_JWT`
- `UNIT_PRICE`

#### Downstream / legacy (prod-like режим)

- `BARCODEGEN_URL`
- `BILLING_URL`
- `AI_URL`
- `HISTORY_URL`
- `AUTH_URL`
- `NOTIFICATIONS_URL`
- `TRANS_HISTORY_URL`

#### Kafka / Redis

- `KAFKA_BROKERS`
- `REDIS_URL`

#### Таймауты

- `BARCODEGEN_TIMEOUT`
- `BILLING_TIMEOUT`
- `AI_TIMEOUT`

Значения таймаутов задаются в **миллисекундах**.

#### Надёжность / флаги

- `IDEMPOTENCY_TTL`
- `ENABLE_PARTIAL_SUCCESS`
- `ENABLE_IDEMPOTENCY`
- `ENABLE_LEGACY_AUTH`
- `ENABLE_NOTIFICATIONS`
- `MAINTENANCE_MODE`

`IDEMPOTENCY_TTL` задаётся в **секундах**. Пример согласно ТЗ: `86400` = 24 часа.

### File-backed конфиги Admin API

Помимо ENV, приложение читает и обновляет фиксированные конфиги из репозитория:

- `configs/revisions/` — YAML-конфиги ревизий;
- `configs/admin/timeouts.yaml` — таймауты downstream-вызовов;
- `configs/admin/topup-bonus.yaml` — настройки бонусов пополнения.

Эти пути зафиксированы в `internal/config/paths.go` и используются при старте API/worker.

#### Observability

- `GRAFANA_ADMIN_USER`
- `GRAFANA_ADMIN_PASSWORD`
- `GRAFANA_ANONYMOUS_ENABLED`

---

## 7. Запуск без Docker

Если хотите запустить API и worker напрямую из исходников:

### Подготовка

#### Windows PowerShell

```powershell
Copy-Item .env.example .env
```

#### Linux / macOS

```bash
cp .env.example .env
```

### Запуск API

#### Windows PowerShell

```powershell
go run ./cmd/api
```

#### Linux / macOS

```bash
go run ./cmd/api
```

### Запуск worker в отдельном окне

#### Windows PowerShell

```powershell
go run ./cmd/worker
```

#### Linux / macOS

```bash
go run ./cmd/worker
```

### Что важно

Если не заданы `BARCODEGEN_URL`, `BILLING_URL`, `AI_URL`, `HISTORY_URL`, `AUTH_URL` и другие внешние URL,
сервис использует встроенные mock-адаптеры.

---

## 8. Запуск в Docker напрямую

### Dev-режим

Dev-режим для локальной разработки с mock downstreams:

- `bff`
- `worker`
- `zookeeper`
- `kafka`
- `redis`

Используйте отдельный dev override, который зануляет внешние service URL и тем самым переключает приложение на встроенные mock-адаптеры.

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
```

> Базовый `docker-compose.yml` содержит service/legacy URL согласно ТЗ.
> Если запускать только его, приложение будет работать в режиме интеграции с внешними сервисами.

### Test-режим

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml -f docker-compose.test.yml up -d --build
```

### Prod-like режим

Использует реальные downstream URL из `.env`:

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
```

### Observability stack отдельно

```bash
docker compose -f docker-compose.yml -f docker-compose.observability.yml up -d prometheus loki promtail grafana
```

### Всё вместе

```bash
docker compose -f docker-compose.yml -f docker-compose.observability.yml up -d --build
```

### Остановить всё

```bash
docker compose -f docker-compose.yml -f docker-compose.observability.yml down --remove-orphans
```

---

## 9. Запуск через Makefile

Если у вас установлен `make`, можно использовать более короткие команды.

### Доступные команды

```bash
make help
make dev-up
make test-up
make prod-up
make obs-up
make up-all
make down
make logs
make ps
make config-dev
make config-test
make config-prod
make config-obs
make test
```

### Что означает каждая команда

- `make dev-up` — локальный docker dev stack с mock downstreams.
- `make test-up` — test stack с отключённым legacy auth и notifications.
- `make prod-up` — prod-like docker stack с реальными внешними сервисами из ENV.
- `make obs-up` — поднимает observability stack: Prometheus + Loki + Promtail + Grafana.
- `make up-all` — приложение + observability stack.
- `make down` — остановка контейнеров.
- `make logs` — tail логов.
- `make ps` — список контейнеров.
- `make config-*` — рендер итоговой compose-конфигурации.
- `make test` — `go test ./... -count=1`.

---

## 10. URL после запуска

### Приложение

- BFF API base URL: `http://localhost:8080`
- Health: `http://localhost:8080/health`
- Metrics: `http://localhost:8080/metrics`

### Observability

- Prometheus UI: `http://localhost:9090`
- Grafana UI: `http://localhost:3000`
- Loki readiness: `http://localhost:3100/ready`
- Loki API example: `http://localhost:3100/loki/api/v1/labels`

### Инфраструктура

- Kafka broker для контейнеров: `kafka:9092`
- Kafka broker для внешних инструментов с хоста: `localhost:9093`
- Redis: `localhost:6379`

### Grafana логин по умолчанию

- user: `admin`
- password: `admin`

Если вы не меняли их через `.env` (`GRAFANA_ADMIN_USER`, `GRAFANA_ADMIN_PASSWORD`).

---

## 11. HTTP API

### Public / client API

| Метод | Путь | Auth | Описание |
|-------|------|------|----------|
| GET | `/health` | — | Health check |
| GET | `/metrics` | — | Prometheus metrics |
| GET | `/api/v1/billing/quote` | User JWT | Проверка доступных средств |
| GET | `/api/v1/revisions` | User JWT | Список доступных ревизий |
| GET | `/api/v1/revisions/:revision/schema` | User JWT | Схема формы ревизии |
| POST | `/api/v1/barcode/generate` | User JWT + `X-Idempotency-Key` | Генерация баркодов |
| POST | `/api/v1/barcode/generate/pdf417` | User JWT + `X-Idempotency-Key` | Dedicated генерация PDF417 |
| POST | `/api/v1/barcode/generate/code128` | User JWT + `X-Idempotency-Key` | Dedicated генерация Code128 |
| GET | `/api/v1/barcode/:id` | User JWT | Получить баркод для remake |
| POST | `/api/v1/barcode/:id/edit` | User JWT + `X-Idempotency-Key` | Бесплатное редактирование |

### Bulk / service API

| Метод | Путь | Auth | Описание |
|-------|------|------|----------|
| POST | `/api/v1/bulk/wake` | Service Token | Проверка активности bulk consumer |
| POST | `/internal/validate` | Service Token | Валидация строки bulk-задачи |
| POST | `/internal/billing/quote` | Service Token | Quote для Bulk Service |
| POST | `/internal/billing/block-batch` | Service Token + `X-Idempotency-Key` | Блокировка batch |

### Admin API

| Метод | Путь | Auth | Описание |
|-------|------|------|----------|
| GET | `/admin/revisions` | Admin JWT | Список ревизий |
| PUT | `/admin/revisions/:revision` | Admin JWT + `X-Idempotency-Key` | Обновить ревизию |
| PUT | `/admin/config/topup-bonus` | Admin JWT + `X-Idempotency-Key` | Обновить бонусы |
| PUT | `/admin/config/timeouts` | Admin JWT + `X-Idempotency-Key` | Обновить таймауты |
| GET | `/admin/kafka/topics` | Admin JWT | Список Kafka topics |

Полная спецификация:
- `docs/openapi.yaml`

---

## 12. Kafka topics

Основные топики, которые использует проект:

- `bulk.tasks`
- `bulk.result`
- `barcode.generated`
- `barcode.edited`
- `billing.saga.completed`
- `billing.saga.partial_completed`
- `notifications.send`
- `trans-history.log`
- `auth.user.info`

---

## 13. Monitoring / Logs / Dashboards

### Метрики

Сервис экспортирует Prometheus-метрики:

- `bff_requests_total`
- `bff_partial_success_total`
- `bff_chain_execution_duration_ms`
- `bff_barcodegen_calls_total`
- `bff_duplicate_requests_total`

### Логи

Для логов используется связка:

- `Grafana` — UI
- `Loki` — log storage
- `Promtail` — сбор логов из Docker

### Готовый дашборд

Автоматически подключается dashboard:

- `monitoring/grafana/dashboards/barcode-bff-overview.json`

В нём есть:

- requests / 5m
- rate вызовов BarcodeGen
- live logs для `bff` и `worker`

---

## 14. Полезные команды после запуска

### Проверка статуса контейнеров

```bash
docker compose -f docker-compose.yml -f docker-compose.observability.yml ps
```

### Просмотр логов

```bash
docker compose -f docker-compose.yml -f docker-compose.observability.yml logs -f --tail=200
```

### Проверка health

#### Windows PowerShell

```powershell
Invoke-WebRequest http://localhost:8080/health | Select-Object -ExpandProperty Content
```

#### Linux / macOS

```bash
curl http://localhost:8080/health
```

### Проверка метрик

#### Windows PowerShell

```powershell
Invoke-WebRequest http://localhost:8080/metrics | Select-Object -ExpandProperty Content
```

#### Linux / macOS

```bash
curl http://localhost:8080/metrics
```

---

## 15. Тесты

Запуск всех тестов:

```bash
go test ./... -count=1
```

Что уже покрыто:

- `internal/integration` — **сквозные integration tests** (HTTP → Usecase → Kafka-события):
  - `TestIntegration_GenerateFlow_FullSuccess` — полный flow (Quote → Block → Generate → Capture → events)
  - `TestIntegration_GenerateFlow_PartialSuccess` — partial billing (5 из 10, partial_completed)
  - `TestIntegration_GenerateFlow_InsufficientFunds` — нет средств → 402
  - `TestIntegration_GenerateFlow_PartialRequiresConfirmation` — partial без confirmed → 200 PARTIAL_FUNDS
  - `TestIntegration_Idempotency_DuplicateRequest` — повторный запрос → DUPLICATE_REQUEST
  - `TestIntegration_GenerateFlow_ValidationError` — отсутствие обязательных полей → 400
  - `TestIntegration_FreeEditFlow` — полный flow бесплатного редактирования + barcode.edited event
- `internal/usecase` — ChainExecutor, GenerateUseCase (AI, retry, compensating tx, notifications), EditUseCase, QuoteUseCase, RevisionSchemaUseCase
- `internal/transport/http/gin` — router tests (все endpoints, idempotency middleware, auth middleware, maintenance mode)
- `internal/transport/kafka` — BulkJobHandler (success, partial failure, empty batch)
- `internal/adapters/billing` — HTTPClient (quote, block, capture, release, blockBatch, dynamic timeout)
- `internal/adapters/barcodegen` — HTTPClient (pdf417, code128, calculate, random, idempotency key forwarding)
- `internal/adapters/auth` — HTTPClient (validate token, invalid token)
- `internal/adapters/idempotency` — MemoryStore (TTL, Reserve/Get/Set, concurrent access)
- `internal/adapters/revisions` — MemoryStore (schema + config)
- `internal/adapters/timeouts` — MemoryStore
- `internal/adapters/topupbonus` — MemoryStore
- `internal/adapters/events` — MockPublisher
- `internal/app` — BuildAPIApp / BuildWorkerApp (DI сборка)

---

## 16. Полезные документы в репозитории

- OpenAPI: `docs/openapi.yaml`
- Architecture map: `docs/architecture/component-map.md`
- ADRs: `docs/adr/*`
- Rollback runbook: `docs/operations/rollback-plan.md`
- Revision configs: `configs/revisions/*`
- Admin configs: `configs/admin/timeouts.yaml`, `configs/admin/topup-bonus.yaml`
- Prometheus config: `monitoring/prometheus.yml`
- Loki config: `monitoring/loki-config.yml`
- Promtail config: `monitoring/promtail-config.yml`

---

## 17. Production замены mock-адаптеров

| Компонент | Dev / local | Production / prod-like |
|-----------|-------------|-------------------------|
| `BillingClient` | `billing.MockClient` | HTTP через `BILLING_URL` |
| `BarcodeGenClient` | `barcodegen.MockClient` | HTTP через `BARCODEGEN_URL` |
| `AIClient` | `ai.MockClient` | HTTP через `AI_URL` |
| `HistoryClient` | `history.MockClient` | HTTP через `HISTORY_URL` |
| `AuthClient` | `auth.MockClient` | HTTP через `AUTH_URL` |
| `IdempotencyStore` | `MemoryStore` | Redis через `REDIS_URL` |
| `BulkJobConsumer` | `MockConsumer` | Kafka через `KAFKA_BROKERS` |
| `EventPublisher` | `MockPublisher` | Kafka producer |

---

## 18. Краткий сценарий для нового разработчика

Если хотите просто поднять проект и посмотреть UI/метрики/логи:

### Вариант 1 — только приложение

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
```

После этого доступны:

- `http://localhost:8080/health`
- `http://localhost:8080/metrics`

### Вариант 2 — приложение + observability

```bash
docker compose -f docker-compose.yml -f docker-compose.observability.yml up -d --build
```

После этого доступны:

- `http://localhost:8080/health`
- `http://localhost:8080/metrics`
- `http://localhost:8080/api/v1/revisions` *(требует Bearer token, если включён legacy auth)*
- `http://localhost:9090`
- `http://localhost:3000`
- `http://localhost:3100/ready`
- `http://localhost:3100/loki/api/v1/labels`

### Вариант 3 — через Makefile

```bash
make up-all
```

---

## 19. Примечание по соответствию ТЗ

README описывает текущее состояние репозитория и режимы запуска согласно ТЗ и согласован с:

- `docs/architecture/component-map.md`
- `Dockerfile`
- `docker-compose.yml`
- `Makefile`

Если вы меняете инфраструктуру или список endpoints, обновляйте этот файл синхронно с кодом.
