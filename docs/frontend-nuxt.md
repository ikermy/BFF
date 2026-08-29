# Рекомендации для Nuxt-фронтенда

Фронтенд (Nuxt) и BFF-API раздаются через **один Envoy** (один origin), поэтому:

- `NUXT_PUBLIC_API_BASE_URL=/api/v1` — **относительный** путь, одинаковый в dev и prod;
- фронт и API находятся на одном домене — CORS не нужен;
- пересоздавать значение при смене окружения не требуется.

---

## 1. Единый относительный API base

Задайте один и тот же путь для всех окружений:

```bash
NUXT_PUBLIC_API_BASE_URL=/api/v1
```

В `nuxt.config.ts`:

```ts
export default defineNuxtConfig({
  runtimeConfig: {
    public: {
      apiBase: process.env.NUXT_PUBLIC_API_BASE_URL || '/api/v1',
    },
  },
})
```

> Не зашивайте абсолютный origin (`http://localhost:8080`, `https://domain`). Он ломается
> при смене dev/prod или домена. Относительный путь работает всегда, т.к. Envoy
> маршрутизирует `/api/v1/*` на BFF.

## 2. Вызов API через composable

```ts
// composables/useApi.ts
export function useApi() {
  const config = useRuntimeConfig()
  return async (path: string, opts: RequestInit = {}) => {
    const token = useCookie('auth_token') // как храните JWT
    return $fetch(`${config.public.apiBase}${path}`, {
      ...opts,
      headers: {
        Authorization: `Bearer ${token.value ?? ''}`,
        ...(opts.body ? { 'Content-Type': 'application/json' } : {}),
      },
    })
  }
}
```

## 3. Обязательные заголовки для генерации

- `Authorization: Bearer <JWT>` — BFF валидирует пользовательский токен по `JWT_SECRET`.
- `X-Idempotency-Key: <uuid>` — обязателен для `POST /barcode/generate*` (идемпотентность,
  п.14.1 ТЗ). Генерируйте на запрос и переиспользуйте при ретрае, чтобы не получить дубли.

## 4. CORS не нужен

Фронт и API — один origin через Envoy → браузер не шлёт preflight-запросы.
CORS в Envoy уже включён как страховка, но на стороне Nuxt настраивать его не требуется.

## 5. Деплой: контейнер `frontend` в сети `barcode_shared`

Nuxt-статика (или SSR) раздаётся nginx-контейнером с именем **`frontend`** на порту **80**
в той же docker-сети, что и envoy. Envoy уже роутит `/` → `frontend:80`.

### Dockerfile (статический экспорт)

```dockerfile
FROM node:20-alpine AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build          # → .output/public

FROM nginx:1.27-alpine
COPY --from=build /app/.output/public /usr/share/nginx/html
EXPOSE 80
```

### Фрагмент docker-compose (добавить к базовому)

```yaml
frontend:
  build: ./frontend
  restart: unless-stopped
  networks:
    - barcode-shared
```

## 6. Эндпоинты BFF, которые зовёт фронтенд

| Эндпоинт | Назначение | Заголовки |
|---|---|---|
| `GET /api/v1/revisions` | список ревизий | Bearer |
| `GET /api/v1/revisions/:rev/schema` | схема полей формы | Bearer |
| `GET /api/v1/billing/quote` | котировка | Bearer |
| `POST /api/v1/barcode/generate` | генерация баркодов | Bearer + `X-Idempotency-Key` |
| `POST /api/v1/barcode/:id/edit` | бесплатное редактирование | Bearer + `X-Idempotency-Key` |
| `GET /api/v1/barcode/:id` | данные баркода (remake) | Bearer |

> `barcodeUrl` в ответе — относительный путь вида `/artifacts/<hash>.png`. Используйте его
> как есть: он того же origin и отдаётся Envoy через `/artifacts/` → bff.

## 7. Пересоздание контейнеров при изменениях

- **Код приложения (bff)** → нужна пересборка/пересоздание образа bff.
- **Конфигурация Envoy / добавление frontend** → пересоздать **Envoy** и добавить
  контейнер **frontend**; контейнер bff пересоздавать не нужно (код не менялся).
- **docker-compose.yml** → перезапустить деплой для синхронизации сети/сервисов.
