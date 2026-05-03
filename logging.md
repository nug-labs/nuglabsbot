# Telegram-v2 logging (`stdout`)

Runtime logs are emitted through `utils.Logger` (`utils/logger.go`) to stdout, with async buffering enabled by `NewAsyncLogger` in `app.go`.

For new process telemetry, log lines use a structured pattern:

- `event=<name>`
- `status=<ok|slow|error|started|stopped>`
- `duration_ms=<int>` where timing is relevant

## Event catalog

### `event=update-handle`

- **Source:** `routes/root.go` (`UpdateRouter.HandleUpdate`)
- **When:** Each update processed (`message`, `command`, `inline`, **`callback_query`**, **`chat_member`**, `other`).
- **Fields:** `kind`, `status`, `duration_ms` (+ `err` when error)

### `event=deferred-write`

- **Source:** `utils/db/deferred.go`
- **When:** Deferred DB write execution is slow or errors
- **Fields:** `status` (`slow`/`error`), `duration_ms`, `pending` (+ `err` on error)

### `event=deferred-write-worker`

- **Source:** `utils/db/deferred.go`
- **When:** Worker stops on context cancellation
- **Fields:** `status=stopped`, `pending`

### `event=analytics-worker`

- **Source:** `use-cases/handle-events/root.go`
- **When:** Analytics background worker starts/stops
- **Fields:** `status` (`started`/`stopped`), `flushed` (on stop)

### `event=analytics-insert`

- **Source:** `use-cases/handle-events/root.go`
- **When:** Insert to `app_analytics` is slow or errors
- **Fields:** `status` (`slow`/`error`), `duration_ms`, `event_name` (+ `err` on error)

### `event=broadcast-run`

- **Source:** `use-cases/handle-broadcast/root.go`
- **When:** One scheduler pass completes
- **Fields:** `status` (`ok`/`slow`), `duration_ms`, `processed`, `sent`, `aborted`

### `event=strain-collection-press`

- **Source:** `use-cases/handle-strain-press/root.go` (invoked from `routes/root.go` on **`scf:`** `callback_query` presses).
- **When:** Confirmation attempts complete.
- **`status`:** **`ok`** (insert + count), **`miss`** (no token row / duplicate consume), **`invalid`** (malformed **`scf:`** payload), **`error`** (DB / scan failure apart from **`miss`**).
- **Typical structured fields:** `user_id`, `strain` (canonical), `encounter_cnt` (logged on **`ok`**), **`token_id`**, **`phase`** (logging inside **`handle-strain`** on token insert/count warnings).
- **Note:** After the callback succeeds, **`NotifyAfterStrainCollected`** may **`SendMessage`** one updated strain card (with Encounter count) plus re-queue subscriber strain pushes (**no** Encounter on subscriber cards).

## Existing warning/error logs kept

- `broadcast message send failed ...`
- `broadcast quiz send failed ...`
- `broadcast run failed ...`
- route-level ensure-user warnings
- `analytics marshal meta ...`

## Grafana notes

- `logging.json` is a Supabase-Postgres dashboard over persisted runtime signals in `app_analytics` and broadcast tables.
- For stdout log exploration, pair this with Loki/Promtail (or another log sink) and reuse the `event=` keys as labels/filters.
