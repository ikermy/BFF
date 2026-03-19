# ADR 0002: Internal/Auth JWT

- Статус: accepted
- Дата: 2026-03-17

## Решение

- Для `/internal/*` используется service JWT (`INTERNAL_SERVICE_JWT`).
- Для `/admin/*` используется отдельный admin JWT (`ADMIN_JWT`).
- Middleware проверяет `Authorization: Bearer <token>`.

## Причины

- Явное разделение привилегий между internal и admin API.
- Простая интеграция с Bulk Service и внутренними сервисами.

