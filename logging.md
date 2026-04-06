# Telegram-v2 logging (`stdout`)

Runtime logs are emitted through `utils.Logger` (`utils/logger.go`) to stdout, with async buffering enabled by `NewAsyncLogger` in `app.go`.

For new process telemetry, log lines use a structured pattern:

- `event=<name>`
- `status=<ok|slow|error|started|stopped>`
- `duration_ms=<int>` where timing is relevant

## Event catalog

### `event=update-handle`

- **Source:** `routes/update.go`
- **When:** Each update processed (`message`, `command`, `inline`, `other`)
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

## Existing warning/error logs kept

- `broadcast message send failed ...`
- `broadcast quiz send failed ...`
- `broadcast run failed ...`
- route-level ensure-user warnings
- `analytics marshal meta ...`

## Grafana notes

- `logging.json` is a Supabase-Postgres dashboard over persisted runtime signals in `app_analytics` and broadcast tables.
- For stdout log exploration, pair this with Loki/Promtail (or another log sink) and reuse the `event=` keys as labels/filters.
