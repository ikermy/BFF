# Rollback Plan

Операционный runbook по п.19.

## Критерии отката
- error rate > 10%
- partial to full ratio < 50%
- BarcodeGen errors > 5%

## Что делает maintenance mode в текущем коде

- флаг `MAINTENANCE_MODE` читается из ENV (`internal/config/config.go`);
- при `MAINTENANCE_MODE=true` middleware возвращает `503` с `code=MAINTENANCE_MODE` для всех маршрутов, кроме `/health` и `/metrics`;
- это позволяет выполнить откат, не теряя readiness/liveness и scrape метрик.

## Процедура

Ниже — процедура из ТЗ для Kubernetes deployment:

```bash
kubectl set env deployment/barcode-bff MAINTENANCE_MODE=true
kubectl rollout undo deployment/barcode-bff
kubectl rollout status deployment/barcode-bff
kubectl set env deployment/barcode-bff MAINTENANCE_MODE=false
```

> Имя deployment `barcode-bff` взято из ТЗ. Kubernetes manifests в текущем репозитории не приложены, поэтому фактическое имя deployment нужно брать из вашего окружения.

## Проверки после отката
1. `GET /health` отвечает `200`.
2. `GET /metrics` отдаёт Prometheus-метрики.
3. Логи не содержат всплеска `BARCODEGEN_ERROR` / `BILLING_ERROR`.
4. Тестовый `GET /api/v1/billing/quote?units=1&revision=US_CA_08292017` проходит успешно.
5. `MAINTENANCE_MODE` возвращён в `false` после завершения rollback.

## Связанные артефакты
- Maintenance middleware: `internal/transport/http/gin/middleware.go`
- Router: `internal/transport/http/gin/router.go`
- Runtime config: `internal/config/config.go`
- Prometheus scrape config: `monitoring/prometheus.yml`
- API spec: `docs/openapi.yaml`

