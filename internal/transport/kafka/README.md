# Kafka Transport

Транспортный слой для обработки входящих Kafka-сообщений (п.14.6 ТЗ).

## Реализовано

### Consumer: `bulk.tasks` → `bulk_job_handler.go`
Обрабатывает входящее сообщение от Bulk Service:
```
bulk.tasks (Kafka)
    └── BulkJobHandler.Handle()
            ├── GenerateUseCase.Execute()
            └── EventPublisher.PublishBulkResult() → bulk.result (Kafka)
```

**Wiring:**
- `KAFKA_BROKERS` задан → `adapters/kafka.Consumer` (реальный `segmentio/kafka-go`, at-least-once)
- `KAFKA_BROKERS` не задан → `adapters/kafka.MockConsumer` (Go-канал, dev/test)

**Consumer group:** `bff-bulk-worker` (фиксирована в коде)

### Producers
Все отправки в Kafka сосредоточены в одном месте: `adapters/events/kafka_publisher.go`

| Топик                          | Метод                    | ТЗ      |
|-------------------------------|--------------------------|---------|
| `bulk.result`                 | `PublishBulkResult`      | п.14.6  |
| `billing.saga.completed`      | `PublishSagaCompleted`   | п.14.4  |
| `billing.saga.partial_completed` | `PublishPartialCompleted` | п.14.4 |
| `barcode.generated`           | `PublishBarcodeGenerated`| п.10.3  |
| `barcode.edited`              | `PublishBarcodeEdited`   | п.10.1  |
| `notifications.send`          | `SendGenerationComplete/Error` | п.11.2 |
| `trans-history.log`           | `LogTransaction`         | п.11.3  |

## Wake endpoint
`POST /api/v1/bulk/wake` — позволяет Bulk Service убедиться что BFF живой и читает Kafka.
Возвращает `pendingMessages` — consumer lag из `kafka.Reader.Stats()`.
