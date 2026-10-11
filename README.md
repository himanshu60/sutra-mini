# Sutra Mini

Fabric goes out to job-workers and comes back in parts. Every receipt creates an accounting
voucher, and every voucher must reach an accounting API that is slow and fails without warning.
This repo is the smallest honest version of the layer that makes that reliable.

```
Next.js /sync ──server-side fetch──► Go API :8080 ──────────────► Postgres (Neon)
                                                                      ▲
Go worker × N ──claim (SKIP LOCKED + lease)───────────────────────────┘
      └──POST /api/vouchers (same client_ref every attempt, 8 s timeout)──► mock accounting :8081
```

- `cmd/api` — receipts, issues, vouchers, manual retry
- `cmd/worker` — pushes pending vouchers; run as many as you like
- `cmd/mockacct` — the misbehaving accounting API (20% HTTP 500, 10% hang 5 s, 409 on a repeated `client_ref`)
- `cmd/migrate` — schema and seed data
- `web/` — the `/sync` page

## Run it

You need **Go 1.25+**, **Node 20.9+**, **curl** and a free **[Neon](https://neon.tech)** project.

1. In Neon, create a project, open **Connect**, switch **Connection pooling off**, and copy the connection string.
2. Set up and migrate:

   ```bash
   git clone https://github.com/himanshu60/sutra-mini && cd sutra-mini
   cp .env.example .env          # paste the Neon string into DATABASE_URL
   go run ./cmd/migrate          # schema + seed: 5 SKUs, 2 job-workers, 4 open issues (ids 1–4)
   ```

   Running `migrate` again is safe; it applies only what is new.
3. Start each of these in its own terminal, from the repo root:

   ```bash
   go run ./cmd/mockacct         # mock accounting API on :8081
   go run ./cmd/api              # Sutra API on :8080
   go run ./cmd/worker           # sync worker; start it in a second terminal to run two
   cd web && cp .env.example .env.local && npm install && npm run dev   # http://localhost:3000/sync
   ```

On Windows PowerShell use `copy` instead of `cp`, and set a variable for one command with
`$env:MOCK_FAIL_RATE="1"; go run ./cmd/mockacct`.

## API

| Method | Path | Result |
|---|---|---|
| POST | `/jobwork/receipts` | `201` created · `201` + `Idempotent-Replayed: true` for a replayed key · `409` would exceed the issued quantity · `422` key reused with a different body · `404` unknown issue · `400` invalid input |
| GET | `/issues/{id}` | issued, received, outstanding |
| GET | `/vouchers?status=pending\|pushed\|failed` | newest 500 vouchers; `status` is optional |
| POST | `/vouchers/{id}/retry` | `200` failed voucher queued again · `409` voucher is not failed · `404` |

```bash
$ curl -i -X POST localhost:8080/jobwork/receipts -H 'Content-Type: application/json' \
    -H 'Idempotency-Key: rcpt-001' -d '{"issue_id":1,"qty":10,"received_on":"2026-10-11"}'
HTTP/1.1 201 Created
{"receipt_id":1,"issue_id":1,"qty":10,"received_on":"2026-10-11","voucher_id":1,"amount_paise":45000,"client_ref":"4b001ebb-9b04-409d-8d30-8c3514bee92c"}

$ # the same request again
HTTP/1.1 201 Created
Idempotent-Replayed: true
{"receipt_id":1,"issue_id":1,"qty":10,"received_on":"2026-10-11","voucher_id":1,"amount_paise":45000,"client_ref":"4b001ebb-9b04-409d-8d30-8c3514bee92c"}

$ # same key, qty 11
HTTP/1.1 422 Unprocessable Entity
{"error":"idempotency key already used with a different request"}

$ # 91 more pieces of issue 1
HTTP/1.1 409 Conflict
{"error":"receipt exceeds outstanding quantity (90 outstanding)","outstanding":90}

$ curl localhost:8080/issues/1
{"id":1,"party_id":1,"sku_id":1,"issued":100,"received":10,"outstanding":90}
```

If the `Idempotency-Key` header is missing, the API makes one up and returns it in the response's
`Idempotency-Key` header, so the caller can still replay safely.

## How the guarantees are enforced

### A receipt and its voucher: one transaction, no overshoot, idempotent

`store.CreateReceipt` ([internal/store/receipts.go](internal/store/receipts.go)) runs three statements in one
`READ COMMITTED` transaction, in this order:

1. **Insert the receipt** with `ON CONFLICT (idempotency_key) DO NOTHING`. The unique index claims the key. A
   concurrent request with the same key waits on that index until the first one commits or rolls back. If no
   row comes back, the request is a replay: the stored receipt and voucher are returned, or `422` if the body differs.
2. **Add to the issue's running total, only if it still fits:**
   `UPDATE jobwork_issues SET received_qty = received_qty + $n WHERE id = $id AND received_qty + $n <= qty`.
   The row lock queues concurrent receipts for one issue, and each re-checks the condition against the latest
   committed total. No row back means overshoot: roll back and return `409`. The column also has
   `CHECK (received_qty BETWEEN 0 AND qty)`, so the database refuses an overshoot even if this guard is removed.
3. **Insert the voucher** (`pending`, `amount_paise = qty × rate_paise`). Any error rolls back all three writes.

Claiming the key *before* touching the issue means a replay never reaches the counter, so it can never be
mis-reported as an overshoot. Money is integer paise (`BIGINT`) everywhere.

### Pushing vouchers

[internal/worker](internal/worker) and [internal/store/vouchers.go](internal/store/vouchers.go):

- **Claim.** One statement leases the next due voucher:
  `UPDATE … SET locked_by = $worker, locked_until = now() + lease, attempts = attempts + 1 WHERE id = (SELECT … FOR UPDATE SKIP LOCKED)`.
  `SKIP LOCKED` keeps two workers claiming at the same moment off the same row. The lease keeps everyone else
  off it while the HTTP call is in flight. A lease that expires, because its worker died, makes the voucher
  claimable again. Leases use the database clock.
- **Push.** The worker sends the voucher's `client_ref`, a UUID fixed when the voucher is created, the same on
  every attempt. The HTTP client times out after 8 s, longer than the mock's 5 s hang, and the lease (15 s) is
  longer than the timeout.
- **Outcome.**
  - `201` → `pushed` with the `external_ref`.
  - **`409` → `pushed` with the original `external_ref` the API returns.**
  - `500`, network error, timeout or malformed body → stays `pending` with `last_error`, and is retried after
    2 s, 4 s, 8 s … (capped at 60 s, half of it random).
  - Other `4xx`, or the 5th failed attempt → `failed` with the reason.
  - Failure writes only apply while the worker still holds the lease.
- **Never posted twice.** No transaction can span our database and someone else's API, so there is always a
  window between "accounting accepted it" and "we recorded that". If the worker dies inside it, the lease
  expires, another worker sends the **same `client_ref`**, gets `409` with the original `external_ref`, and
  stores it. Delivery is at-least-once, and the accounting side's dedupe makes the result exactly one entry.
- **One slow push does not block others.** Each worker runs `WORKER_CONCURRENCY` (4) independent
  claim → push → record loops.
- **Shutdown.** The first Ctrl+C stops claiming and lets in-flight pushes finish and record. A second Ctrl+C
  exits at once.

A failed voucher goes back to `pending` only through `POST /vouchers/{id}/retry` (the Retry button). That
resets `attempts` to 0 for a fresh round with backoff, and pushes the same row with the same `client_ref`.

## Tests

```bash
go test ./...
```

The tests need `TEST_DATABASE_URL`: any throwaway Postgres 14+, read from `.env` or your shell. Each test
creates its own schema, migrates it and drops it, so a Neon branch works as well as a local server
(`docker run -d -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:17` with the URL from `.env.example`).
Without it the integration tests fail with a message saying so, rather than reporting `ok` for tests that never ran.

| Test | Proves |
|---|---|
| `TestConcurrentReceiptsNeverOvershoot` | 10 receipts of 3 released together against an issue of 10: exactly 3 succeed, `received_qty` = sum of receipts = 9, 3 vouchers, rejected requests write nothing |
| `TestReplayedKeyCreatesNothing` | replaying a key returns the identical body and leaves 1 receipt, 1 voucher, `received_qty` not doubled |
| `TestConcurrentSameKeyCreatesOneReceipt` | 10 simultaneous requests with one key → one receipt, all get the same answer |
| `TestReusedKeyWithDifferentBodyIsRejected` | same key, different qty → `422`, nothing written |
| `TestCrashAfterPushIsReconciledThrough409` | a worker dies after accounting accepted the voucher; the next worker stores the original ref via `409` |
| `TestTimeoutThenReconciled` | the client times out while accounting still records the voucher; the retry reconciles via `409` |
| `TestTwoWorkersPushEachVoucherOnce` | two workers, 30 vouchers, scripted 500s and hangs → 30 accounting entries, 0 duplicates sent |
| `TestLiveLeaseIsNotClaimed`, `TestServerErrorIsRetriedAfterBackoff`, `TestFailsAfterMaxAttemptsThenManualRetry`, `TestSlowPushDoesNotBlockOthers` | lease respected, backoff, `failed` with reason then manual retry, hang isolation |
| `internal/mockacct` tests | a repeated `client_ref` gets `409` even when that request's roll was a 500; concurrent posts record once |

Every test fails when its guarantee is removed. This was checked by breaking the code on purpose:

- Dropping the `WHERE` guard and the `CHECK` → *"OVERSHOOT: 30 received against 10 issued"*.
- Dropping the unique key → 2 receipts and 2 vouchers.
- Ignoring the lease in the claim → *"refused 6 duplicates, want 0"*.
- Treating `409` as an error → the voucher ends `failed`.

## Try the evaluation scenarios

With the mock, API and a worker running:

```bash
# Replay: run twice; the second answer has Idempotent-Replayed: true and the same ids
curl -i -X POST localhost:8080/jobwork/receipts -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-1' -d '{"issue_id":1,"qty":5,"received_on":"2026-10-11"}'

# Ten concurrent receipts on issue 3 (20 issued): six 201s, four 409s
for i in $(seq 1 10); do
  curl -s -o /dev/null -w '%{http_code}\n' -X POST localhost:8080/jobwork/receipts \
    -H 'Content-Type: application/json' -H "Idempotency-Key: burst-$i" \
    -d '{"issue_id":3,"qty":3,"received_on":"2026-10-11"}' &
done; wait
curl localhost:8080/issues/3          # received 18, outstanding 2
```

**Kill the worker during the 5-second hang.**

1. Restart the mock with `MOCK_FAIL_RATE=0 MOCK_HANG_RATE=1` so every new voucher hangs.
2. Post a receipt. When the mock logs `recorded, hanging … external_ref=ACC-…`, kill the worker hard: `kill -9`,
   or Ctrl+C twice. A single Ctrl+C is graceful and would let the push finish.
3. Start the worker again. Once the dead worker's lease runs out (≤ 15 s), the mock logs `duplicate refused`,
   the worker logs `already in accounting: 409 reconciled`, and the voucher is `pushed` with the same
   `external_ref`. The mock holds one entry for it.

**Two workers.** Start a second `go run ./cmd/worker`, post a batch of receipts, and watch both workers' logs:
each `client_ref` appears in the mock's `created` lines once.

**Failed → Retry.**

1. Start the mock with `MOCK_FAIL_RATE=1` and the worker with `WORKER_MAX_ATTEMPTS=2`. Post a receipt: after two
   500s the voucher is `failed` with the reason.
2. Restart the mock normally and press **Retry** on `/sync`. The voucher is pushed.

## Configuration

All variables are optional except the database URLs. See [.env.example](.env.example).

| Variable | Default | Used by |
|---|---|---|
| `DATABASE_URL` | — | migrate, api, worker |
| `TEST_DATABASE_URL` | — | `go test` |
| `API_ADDR` / `MOCK_ADDR` | `:8080` / `:8081` | api / mockacct |
| `ACCOUNTING_URL` | `http://localhost:8081` | worker |
| `MOCK_FAIL_RATE`, `MOCK_HANG_RATE`, `MOCK_HANG_DURATION` | `0.2`, `0.1`, `5s` | mockacct |
| `WORKER_CONCURRENCY`, `WORKER_MAX_ATTEMPTS` | `4`, `5` | worker |
| `WORKER_HTTP_TIMEOUT`, `WORKER_LEASE` | `8s`, `15s` (lease must be longer) | worker |
| `SUTRA_API_URL` (in `web/.env.local`) | `http://localhost:8080` | Next.js server only, never sent to the browser |

## Changes to the given schema

All of these are in [migrations/0001_init.sql](migrations/0001_init.sql).

- `jobwork_issues.rate_paise` is the agreed job-work charge per piece; the voucher amount is qty × rate. See assumptions.
- `jobwork_issues.received_qty` with `CHECK (received_qty BETWEEN 0 AND qty)` is the running total that makes
  overshoot impossible in the database.
- `vouchers.next_attempt_at`, `locked_by`, `locked_until` hold backoff scheduling and the worker lease.
- Constraints: `client_ref UUID UNIQUE DEFAULT gen_random_uuid()`, `UNIQUE (type, source_id)` (one voucher per
  receipt), `CHECK (status <> 'pushed' OR external_ref IS NOT NULL)`, positive quantities, status enum.

## Assumptions and limitations

- **Voucher amount.** The brief gives no pricing rule, so I assumed a per-piece job-work rate agreed on each issue.
  The seed data uses made-up rates.
- **Idempotency stores successes only.** A rejected request (`409`, `422`, `400`) writes nothing, so replaying its
  key re-evaluates it. That stays consistent because outstanding quantity only ever goes down.
- **Effectively-once needs the accounting side to dedupe `client_ref`.** The mock does. If the real API cannot, the
  worker would have to look a voucher up by `client_ref` before re-posting.
- **The mock keeps its state in memory.** Restarting it forgets which refs it has seen. Its external refs carry a
  random per-run tag (`ACC-7KQ2-000001`) so refs from different runs never look alike.
- **A crashed worker's voucher waits for its lease** (15 s), and the crash counts as one attempt.
- No authentication on the API, the dashboard or its server action. There are no corrections or reversals of
  receipts, `received_on` is not checked against the issue date, and `GET /vouchers` returns the newest 500
  without paging.
- The race detector was not run during development (Windows without a 64-bit C compiler); `go test -race ./...`
  where cgo is available.

## Layout

```
cmd/{api,worker,mockacct,migrate}   entry points
internal/store                      SQL: receipts, issues, vouchers
internal/api                        HTTP handlers (+ receipt tests)
internal/worker                     sync worker and accounting client (+ worker tests)
internal/mockacct                   mock accounting API (+ tests)
internal/db, internal/config        pool + migration runner, env
internal/testdb                     schema-per-test helper
migrations/                         0001_init.sql, 0002_seed.sql
web/app/sync                        the /sync page, Retry server action
```
