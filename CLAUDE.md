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

> **Локально пользователь не запускает.** Проверка перед коммитом — только `go build ./... && go vet ./... && go test ./...`. Smoke-test и end-to-end проверки выполняются на VPS после `git push` + `./deploy.sh`. Не предлагать запуск через `go run` или local-compose как часть верификации.
>
> **Миграции дополняем на месте.** Реальных пользователей в БД пока нет; перед деплоем БД может быть снесена и применена заново. Поэтому при необходимости небольших добавлений к схеме (новые индексы / nullable-колонки) — расширяем существующую миграцию, а не плодим новый файл `00NNN_*.sql`. Новый файл — только для фундаментально новых таблиц.

Миграции в формате **goose** (маркеры `-- +goose Up` / `-- +goose Down` в `migrations/*.sql`). Локально применяются так:

```bash
goose -dir migrations postgres "postgres://postgres:postgres@127.0.0.1:5433/control_plane?sslmode=disable" up
```

В docker-compose миграции применяются автоматически сервисом `migrate` (см. `docker-compose.yml`, стейдж `migrate` в `Dockerfile`), который ждёт healthy `postgres` и выполняет `goose up`.

После свежего `goose up` **обязательно** засеять справочник `payment_methods` (миграции его не наполняют — БД ещё в стадии без реальных юзеров, эта строка перейдёт в seed-таск, когда появятся):

```sql
INSERT INTO payment_methods (id, name) VALUES
    ('crypto',         'Криптовалюта (CryptoCloud)'),
    ('sbp',            'СБП (Platega)'),
    ('crypto_platega', 'Криптовалюта (Platega)')
ON CONFLICT (id) DO NOTHING;
```

Без этого TG-воронка покажет «Не удалось загрузить методы оплаты».

Правки `.proto`: `api/control/control.proto` — источник; `control.pb.go` и `control_grpc.pb.go` сгенерированы (регенерировать через `protoc --go_out --go-grpc_out`, `go_package = control/api/control;controlpb`).

**Тесты:** table-driven, `testify`. Моки — `minimock`, `genmock.go` лежит на уровне пакета (рядом с тестируемым кодом), сгенерированные файлы — в подпапке `./mocks/`. Папку `mocks/` нужно создавать руками перед первым `go generate` — minimock её сам не создаёт.

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

Домен задаётся в `Caddyfile`. Public endpoints:
- TG webhook: `POST https://<домен>/tg/webhook`
- CryptoCloud callback: `POST https://<домен>/api/control/payment/cryptocloud/webhook`
- **Platega callback: `POST https://<домен>/api/control/payment/platega/webhook` — URL надо однократно прописать в ЛК Platega (Настройки → Callback URLs); в `transaction/process` он не передаётся.**

## Связь с агентом

Агент VPN-ноды живёт в отдельной репе: **`C:\Users\Handi\GolandProjects\windwalker-agent`** (Go module `agent`, у него тоже есть `CLAUDE.md`). Это клиент к нашему gRPC `vpn.control.v1.ControlPlane.Workstream`.

Сводка контракта (детали — в CLAUDE.md агента):
- Агент держит **один** долгоживущий bidi-стрим. Первый фрейм — `AgentHello`, в ответ — `Welcome{agent_id}`. После этого CP может слать `Task` (`Upsert` / `Remove` / `StatsAll` / `StatsUser`).
- Каждый `Task` несёт `TaskMeta{seq, request_id}`. Агент **обязан** ответить `Ack{seq}` или `Nack{seq, err}`. `seq` — монотонный per-agent (наша таблица `agent_seq`).
- Агент идемпотентен по `last_applied_seq` (хранит локально в sqlite через `PRAGMA user_version`-схему, см. его `internal/storage`). Повторный приход того же `seq` → no-op + Ack. Поэтому при реконнекте мы безопасно пере-шлём pending-таски через `RecoverPending`.
- Identity юзера в Xray — `user.ID` как UUID, и он же используется как Xray `email`-поле. `driver_xray.protocol` — только `vless` или `vmess`. Это **обязательство CP** при формировании `UserUpsertRequest`.
- `request_id` в нашем флоу = `subscription_id`. Агент не интерпретирует — пробрасывает обратно в `Response.Meta` (если потерял — CP восстановит через `GetRequestIDByAgentSeq(agent_id, seq)`).
- Heartbeats: агент шлёт каждые `heartbeat_period` сек; CP закрывает стрим с `DeadlineExceeded` если пропуск.

Когда трогаешь `api/control/control.proto` — **обязательно** регенерируй на обеих сторонах. У агента: `protoc --go_out=. --go-grpc_out=. api/control/control.proto`. Несовпадение proto = сломанный стрим.

## Архитектура

Это **control plane** для VPN-сервиса: Telegram-магазин + payment processor, который выдаёт VPN-подключения, отправляя команды **агентам** через bidi-gRPC-стрим. Один бинарь `cmd/main.go` поднимает четыре сервера через `errgroup`: HTTP (chi), gRPC, Telegram webhook и outbox-воркер.

### Сквозной поток

1. **Telegram-бот** (`internal/transport/telegram-bot`) ведёт воронку покупки: регион → протокол → срок → метод оплаты. `ShopService.CreateInvoice` вызывает `Payments.CreatePaymentOrder` (роутится по `methodID` в конкретный `payment.Provider` — например `cryptocloud` для крипты CryptoCloud, `platega` для СБП и крипты Platega), сохраняет `Invoice` со статусом `created` (включая `provider_order_id` — uuid транзакции от провайдера) и возвращает checkout-URL.
2. **Webhook провайдера** (HTTP, `internal/transport/handler`) принимает колбэк платежа → `ProcessService.ProcessPaymentCallback`:
   - `VerifyCallback` на фасаде `Payments` (CryptoCloud — JWT в теле; Platega — `X-MerchantId` + `X-Secret` в заголовках, `subtle.ConstantTimeCompare`). На bad-signature handler Platega отвечает `401` (без ретраев), на transient-ошибки — `500` (Platega ретрайнет до 3 раз с интервалом 5 минут).
   - Внутри `WithTx`: `GetInvoiceByProviderOrder(provider, provider_order_id)` (`SELECT … FOR UPDATE`), идемпотентный short-circuit если уже `success`. Если callback пришёл с `CANCELED/CHARGEBACKED/UNKNOWN` — пишем статус и выходим (подписку не активируем). На `success` — ставим статус, сохраняем `invoice_paid_notification` в outbox, создаём `Subscription`, пишем `subscription_activated` в outbox (**транзакционный outbox**).
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

- `request_id` (= `subscription_id` для subscribe-флоу) = **бизнесовый** ключ идемпотентности. Агент может его не понимать, но обязан echo'ить обратно.
- `(agent_id, seq)` однозначно идентифицирует транспортную операцию; агент обязан обрабатывать строго по порядку и ack'ать по `seq`. Дедуп на стороне агента — через `last_applied_seq` (см. `windwalker-agent/internal/storage`).
- Один и тот же `(agent_id, seq)` может быть переотправлен после реконнекта агента — агент должен быть идемпотентен по `(request_id, seq)`.
- Webhook инвойса идемпотентен через short-circuit `inv.Status == SuccessInvoiceStatus`. Лукап инвойса по callback'у — единый для всех провайдеров: `(payment_provider, provider_order_id)`. Это требует индекс `idx_invoices_provider_order` (UNIQUE partial). `provider_order_id` = идентификатор транзакции на стороне провайдера (`uuid` у CryptoCloud, `transactionId` у Platega). `CreateOrderOutput.ProviderOrderID` ставится при создании ордера и сохраняется в `invoices.provider_order_id`.
- **Доставка кредов отвязана от работы агента**: CP-side ошибка при обработке ответа (DB-недоступна, TG упал и т.п.) **не** ведёт к Nack'у таски. Таска остаётся `done_at IS NULL` и подхватится `RecoverPending` при реконнекте. Доставка пользователю ретраится внутри outbox-воркера.

### Распространение транзакций

`pgx.TxManager.WithTx` прячет `pgx.Tx` в `context.Context`; каждый storage-метод вызывает `s.getExecutor(ctx)` — возвращает tx если есть, иначе пул. **Всегда вызывай storage-методы с tx-ctx** внутри колбэков `WithTx` — иначе молча распадёшь логическую транзакцию. Вложенные `WithTx` переиспользуют текущую tx (без savepoints).

### Слои

- `cmd/main.go` — composition root; вся проводка тут, не в пакетных init'ах.
- `internal/model` — чистые доменные типы + JSON-payload'ы для outbox / agent_tasks. Без I/O, без фреймворков. `CallbackInput.Headers` — `map[string]string` (не `http.Header`), чтобы не тащить `net/http` в модель.
- `internal/service` — use-cases. Каждый сервис определяет **свои узкие storage/collaborator-интерфейсы** прямо над структурой — конкретный `pgx.Storage` их все удовлетворяет. Добавляя метод сервиса, расширяй интерфейс у консьюмера, не в центральном месте.
- `internal/storage/pgx` — реализации на pgx. Используется `scany` для скана строк. `db:`-теги на моделях рулят сканом.
- `internal/transport/{agent,handler,telegram-bot}` — адаптеры gRPC, HTTP, Telegram. Конвертеры (proto ↔ model) живут рядом с транспортом, не в `model`. `handler` извлекает HTTP-headers и кладёт в `CallbackInput.Headers map[string]string` — `net/http` дальше handler'а не идёт.
- `internal/payment` — реестр провайдеров. Каждый провайдер сам решает, как идентифицировать транзакцию: возвращает `ProviderOrderID` в `CreateOrderOutput` (он сохранится в `invoices.provider_order_id`) и кладёт его же в `CallbackOutput.PaymentOrderID` при `VerifyCallback`. Новый провайдер = реализовать `payment.Provider` (`Name()` / `Methods() []string` / `CreatePaymentOrder` / `VerifyCallback`) и передать в `NewPayments(...)` в `main.go`. Активные провайдеры:
  - `cryptocloud` — крипта, JWT в теле postback'а, methodID `"crypto"`.
  - `platega` — СБП (`paymentMethod=2`) и крипта (`=13`), Auth = `X-MerchantId` + `X-Secret` headers (constant-time compare). methodID `"sbp"`, `"crypto_platega"`. Mapping `methodID → numeric paymentMethod` живёт **только** внутри пакета `internal/payment/platega/`.
- `internal/workers` — outbox-воркер. Новый тип события: добавь константу в `model/outbox.go`, payload-тип, case в `Worker.resolveEvent`. При необходимости ретрай-эскалаций — пример в `maybeEscalate` (creds_delivery → delivery_failed_notification).
- `api/control` — protobuf-контракт с агентами. `go_package = control/api/control;controlpb`. **Зеркалится в `windwalker-agent/api/control/`** — при изменении регенерировать в обоих репах.

### Конфиг

`internal/config` использует `cleanenv` (YAML + env-оверрайды). Переменная `CONFIG_PATH` **обязательна** — отсутствующий/невалидный файл = `log.Fatal`. В репозитории два YAML:
- `config/config.yaml` — для локального запуска (`127.0.0.1:5433` в DSN).
- `config/config.docker.yaml` — для docker-compose (`postgres:5432` в DSN, `webhook_public_url` боевой домен). Именно он монтируется в `control-plane`-контейнер как `/app/config.yaml`.

Оба файла закоммичены с реальными секретами — репа приватная, пользователь осознанно так хранит. Не ругай, просто не распространяй дальше. **Секреты Platega (`merchant_id`/`secret`)** в коммите оставлены пустыми — заполнить из ЛК Platega перед первым прогоном.

## Известные шероховатости и ограничения

- `Agent.Validate()` — no-op-заглушка.
- `HandleRemoveCallback` / `HandleStatsAll` / `HandleError` возвращают `"not implemented yet"`.
- В дефолтном конфиге `http.port` и `telegram.port` оба равны `:8081` — поле `telegram.port` в рантайме не используется (webhook примонтирован на chi-роутере по `/tg/webhook`).
- `internal/storage/pgx/proccess.go::GetInvoiceForUpdate` после унификации callback-флоу не вызывается ни откуда — orphan, оставлен как утилита; удалить, если не понадобится в течение пары итераций.
- **Chargeback после success — silent skip.** `ProcessPaymentCallback` идемпотентен на условии `inv.Status == SuccessInvoiceStatus`, поэтому `CHARGEBACKED` от Platega после уже-confirmed транзакции **не** инициирует отзыв подписки. Когда понадобится корректная обработка возвратов: добавить выпуск `subscription_cancelled` outbox-события + перевод инвойса в новый статус. Не делать без явного запроса — задевает business-flow.
- **CANCELED/CHARGEBACKED не уведомляет пользователя.** `ProcessPaymentCallback` пишет статус и выходит — TG-юзер сидит и ждёт креды, которых не будет. Когда понадобится — добавить новый outbox event-type (`invoice_canceled_notification`) и handler в `Worker.resolveEvent`.
- **Money — целые рубли.** `Money.Amount int64` без копеек. Если появится тариф «199.50 ₽», надо переводить всю модель на минорные единицы (изменения в plans-сидинге, `cryptocloud.toMinorUnits`, выводе сумм в TG, в `platega/provider.go::float64(input.Money.Amount)`).