# CLAUDE.md

Этот файл содержит инструкции для Claude Code (claude.ai/code) при работе с этим репозиторием.

## Команды

Запуск / сборка / проверка — из корня репо:

```bash
# Запуск сервиса (CONFIG_PATH обязателен — MustLoadConfig паникует без него)
CONFIG_PATH=config/config.yaml go run ./cmd

go build ./...
go vet ./...
go test ./...                              # все тесты
go test ./internal/service/... -run TestX  # один пакет / один тест
```

Миграции в формате **goose** (маркеры `-- +goose Up` / `-- +goose Down` в `migrations/*.sql`). Локально применяются так:

```bash
goose -dir migrations postgres "postgres://postgres:postgres@127.0.0.1:5433/control_plane?sslmode=disable" up
```

В docker-compose миграции применяются автоматически сервисом `migrate` (см. `docker-compose.yml`, стейдж `migrate` в `Dockerfile`), который ждёт healthy `postgres` и выполняет `goose up`.

Правки `.proto`: `api/control/control.proto` — источник; `control.pb.go` и `control_grpc.pb.go` сгенерированы (регенерировать через `protoc --go_out --go-grpc_out`, `go_package = control/api/control;controlpb`).

## Деплой

Однокнопочный деплой на VPS (Ubuntu, docker установлен через `get.docker.com`):

```bash
git clone <repo> && cd <dir>
cp .env.example .env                 # отредактировать ACME_EMAIL / postgres creds
./deploy.sh                          # git pull + docker compose build + up -d
```

Стек compose:
- **postgres** — Postgres 16, volume `pgdata`, забинден на `127.0.0.1:5432` (наружу не виден; подключаться к БД из GoLand — через SSH-туннель на VPS).
- **migrate** — one-shot goose, ждёт healthy postgres.
- **control-plane** — Go-бинарь на distroless, публикует `:50051` (gRPC для агентов), `:8081` только во внутренней сети.
- **caddy** — авто-TLS Let's Encrypt на домен из `Caddyfile`, `:80` и `:443` наружу, реверс-прокси на `control-plane:8081`.

Домен задаётся в `Caddyfile`. Telegram webhook: `POST https://<домен>/tg/webhook`. CryptoCloud callback: `POST https://<домен>/api/control/payment/cryptocloud/webhook`.

## Архитектура

Это **control plane** для VPN-сервиса: Telegram-магазин + payment processor, который выдаёт VPN-подключения, отправляя команды **агентам** через bidi-gRPC-стрим. Один бинарь `cmd/main.go` поднимает четыре сервера через `errgroup`: HTTP (chi), gRPC, Telegram webhook и outbox-воркер.

### Сквозной поток

1. **Telegram-бот** (`internal/transport/telegram-bot`) ведёт воронку покупки: регион → протокол → срок → метод оплаты. `ShopService.CreateInvoice` вызывает `Payments.CreatePaymentOrder` (роутится по `methodID` в конкретный `payment.Provider`, например `cryptocloud`), сохраняет `Invoice` со статусом `created` и возвращает checkout-URL.
2. **Webhook провайдера** (HTTP, `internal/transport/handler`) принимает колбэк платежа → `ProcessService.ProcessPaymentCallback`:
   - `VerifyCallback` на фасаде `Payments`
   - Внутри `WithTx`: `SELECT … FOR UPDATE` по инвойсу, ставим `success`, сохраняем событие `EventTypeInvoicePaidNotification` в outbox (уведомление «счёт оплачен, ждите»), создаём `Subscription`, пишем `SubscriptionActivatedEvent` в `outbox_events` (**транзакционный outbox**).
3. **Outbox worker** (`internal/workers`) опрашивает `outbox_events` раз в 3 секунды батчами по 5 (лимит попыток — 10). Обрабатывает пять типов событий:
   - `subscription_activated` → `AgentSender.StartUserSubscribe` (выбор агента + dispatch upsert)
   - `subscription_cancelled` → `AgentSender.StopUserSubscribe`
   - `invoice_paid_notification` → отправка TG-сообщения юзеру через `TelegramSender`
   - `creds_delivery` → отправка готового `vless://…` (payload события уже содержит `chat_id` и `message` — второй DB-лукап не нужен)
   - `delivery_failed_notification` → «не получилось, напиши в поддержку», рождается воркером при исчерпании ретраев у `creds_delivery`
4. **AgentSender.StartUserSubscribe** — внутри tx: `ChooseBestAgent` (регион + driver_type, least-loaded), `BindSubscriptionToAgent`, `Dispatcher.DispatchUpsert`. После коммита — `TryDispatchPrepared` пушит таску сразу.
5. **Dispatcher** (`internal/service/dispatcher.go`) — durable pipe команд. `EnqueueTask` пишет в `agent_tasks` с монотонным `seq` из `NextSeq` (бэкенд — таблица `agent_seq`, миграция `00006`). `TrySend` — non-blocking через `sendCh` сессии; фейл инкрементит retries, но строка остаётся pending.
6. **Agent gRPC Workstream** (`internal/transport/agent/server.go`):
   - Агент открывает bidi-стрим, первый фрейм **обязан** быть `AgentHello` → `RegisterAgent` → создаёт `session` в `Hub` (вытесняя stale-сессию с тем же `agent_id`).
   - Сервер отвечает `Welcome`, затем `flushPending` запускает `RecoverPending`: забирает неотаканные `agent_tasks` с `lastSeq`, шлёт по порядку, маркирует `sent_at`.
   - Ответы клиента обновляют `agent_tasks` через `HandleAck` / `HandleNack`. Heartbeats сбрасывают per-stream `hbTimer`; пропуск heartbeat → стрим закрывается с `DeadlineExceeded`.
   - На `HandleStartUserSubscribeResponse` (upsert с кредами): CP **сохраняет креды в `subscriptions.creds`**, пишет `creds_delivery` в outbox, **всегда Ack'ает** таску агента. Доставка пользователю — асинхронно через воркер (не блокирует агента).
   - Если `meta.request_id` в ответе пустой — fallback через `GetRequestIDByAgentSeq(agent_id, seq)` поднимает реальный `subscription_id` из `agent_tasks`.

### Контракт идемпотентности и порядка

- `request_id` (= `subscription_id` для subscribe-флоу) = **бизнесовый** ключ идемпотентности.
- `(agent_id, seq)` однозначно идентифицирует транспортную операцию; агент обязан обрабатывать строго по порядку и ack'ать по `seq`.
- Один и тот же `(agent_id, seq)` может быть переотправлен после реконнекта агента — агент должен быть идемпотентен по `(request_id, seq)`.
- Webhook инвойса идемпотентен через short-circuit `status == SuccessInvoiceStatus`.
- **Доставка кредов отвязана от работы агента**: CP-side ошибка при обработке ответа (DB-недоступна, TG упал и т.п.) **не** ведёт к Nack'у таски. Таска остаётся `done_at IS NULL` и подхватится `RecoverPending` при реконнекте. Доставка пользователю ретраится внутри outbox-воркера.

### Распространение транзакций

`pgx.TxManager.WithTx` прячет `pgx.Tx` в `context.Context`; каждый storage-метод вызывает `s.getExecutor(ctx)` — возвращает tx если есть, иначе пул. **Всегда вызывай storage-методы с tx-ctx** внутри колбэков `WithTx` — иначе молча распадёшь логическую транзакцию. Вложенные `WithTx` переиспользуют текущую tx (без savepoints).

### Слои

- `cmd/main.go` — composition root; вся проводка тут, не в пакетных init'ах.
- `internal/model` — чистые доменные типы + JSON-payload'ы для outbox / agent_tasks. Без I/O, без фреймворков.
- `internal/service` — use-cases. Каждый сервис определяет **свои узкие storage/collaborator-интерфейсы** прямо над структурой — конкретный `pgx.Storage` их все удовлетворяет. Добавляя метод сервиса, расширяй интерфейс у консьюмера, не в центральном месте.
- `internal/storage/pgx` — реализации на pgx. Используется `scany` для скана строк. `db:`-теги на моделях рулят сканом.
- `internal/transport/{agent,handler,telegram-bot}` — адаптеры gRPC, HTTP, Telegram. Конвертеры (proto ↔ model) живут рядом с транспортом, не в `model`.
- `internal/payment` — реестр провайдеров, ключ — и имя провайдера (для роутинга webhook'ов), и id метода (для создания заказа). Новый провайдер = реализовать `payment.Provider` и передать в `NewPayments(...)` в `main.go`.
- `internal/workers` — outbox-воркер. Новый тип события: добавь константу в `model/outbox.go`, payload-тип, case в `Worker.resolveEvent`. При необходимости ретрай-эскалаций — пример в `maybeEscalate` (creds_delivery → delivery_failed_notification).
- `api/control` — protobuf-контракт с агентами. `go_package = control/api/control;controlpb`.

### Конфиг

`internal/config` использует `cleanenv` (YAML + env-оверрайды). Переменная `CONFIG_PATH` **обязательна** — отсутствующий/невалидный файл = `log.Fatal`. В репозитории два YAML:
- `config/config.yaml` — для локального запуска (`127.0.0.1:5433` в DSN).
- `config/config.docker.yaml` — для docker-compose (`postgres:5432` в DSN, `webhook_public_url` боевой домен). Именно он монтируется в `control-plane`-контейнер как `/app/config.yaml`.

Оба файла закоммичены с реальными секретами — репа приватная, пользователь осознанно так хранит. Не ругай, просто не распространяй дальше.

## Известные шероховатости (не «чинить», пока не попросили)

- `Agent.Validate()` — no-op-заглушка.
- `HandleRemoveCallback` / `HandleStatsAll` / `HandleError` возвращают `"not implemented yet"`.
- Провайдер `freekassa` реализован, но не подключён в `main.go`.
- В дефолтном конфиге `http.port` и `telegram.port` оба равны `:8081` — поле `telegram.port` в рантайме не используется (webhook примонтирован на chi-роутере по `/tg/webhook`).
- `cryptocloud/provider.go:105` — `go vet` ругается на формат slog (`%w` в сообщении вместо key/value). Претензия справедливая, но правка не в скоупе.
