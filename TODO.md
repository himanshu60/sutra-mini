# Sutra Mini — build checklist

Ticked only after the acceptance check passed. P0 = required, P1 = important, P2 = stretch.

## Phase 0 — Environment
- [x] P0 Go installed (go1.27.0; module targets go 1.25) — `go version`
- [x] P0 Local PostgreSQL 17.11 for tests (`C:\Users\DELL\pgsql17`, DB `sutra_test`, localhost only) — `pg_ctl status`
- [ ] P0 Neon project created, direct URL in `.env` as `DATABASE_URL` — migrate connects
- [x] P0 `go.mod`, pgx/v5 + godotenv, `.gitignore`, `.env.example` — `.env` not in `git status`

## Phase 1 — Schema
- [ ] P0 `migrations/0001_init.sql`, `0002_seed.sql` — applies on local PG ✔ and Neon (pending URL)
- [x] P0 `internal/db` (Connect, Migrate) + `cmd/migrate` — second run applies nothing; 5 SKUs / 2 parties / 4 issues; CHECK rejects overshoot (23514)

## Phase 2 — Receipt API
- [x] P0 `internal/store` CreateReceipt + GetIssue — vet clean
- [x] P0 `internal/api` + `cmd/api` — curl: 201, replay same body, overshoot 409, key reuse 422, unknown issue 404, bad input 400; rejected requests wrote nothing

## Phase 3 — Mandatory tests
- [x] P0 `internal/testdb` schema-per-test helper
- [x] P0 Test A: concurrent receipts never overshoot (`TestConcurrentReceiptsNeverOvershoot`)
- [x] P0 Test B: idempotency-key replay creates nothing (`TestReplayedKeyCreatesNothing`, `TestConcurrentSameKeyCreatesOneReceipt`, `TestReusedKeyWithDifferentBodyIsRejected`)
- [x] P0 Mutation check: guard+CHECK removed → Test A fails; unique key removed → Test B fails; guard alone removed → CHECK backstop holds (now 409, was 500)

## Phase 4 — Mock accounting
- [x] P0 `internal/mockacct` + `cmd/mockacct` — duplicate client_ref → 409 with original external_ref; 500 calls gave 21% 500s, 9% hangs
- [x] P1 Unit tests: duplicate wins over the random 500; 20 concurrent posts of one client_ref record it once

## Phase 5 — Worker + voucher endpoints
- [x] P0 `store` ClaimVoucher / MarkPushed / MarkRetry / MarkFailed / ListVouchers / RetryVoucher
- [x] P0 `internal/worker` + `cmd/worker` (timeouts, backoff, leases, shutdown) — live: 500 → backoff → failed with reason → manual retry → pushed
- [x] P0 `GET /vouchers?status=`, `POST /vouchers/{id}/retry` — 400 bad status, 404 missing, 409 not failed
- [x] P1 Worker tests (7): crash→409 reconcile, live lease respected, 500→backoff→pushed, exhausted→failed→manual retry, two workers no duplicates, hang isolation, timeout→409 reconcile; mutation-checked (lease removed, 409 not treated as success)

## Phase 6 — Next.js `/sync`
- [x] P0 Page grouped by status, attempts, failure reason, Retry button, loading/empty/error states — tsc, eslint, `next build` clean; rendered over HTTP; Retry server action exercised (accepted / refused / API down). Still to do: one click in a real browser (Himanshu)
- [x] P1 Auto-refresh (3 s, `router.refresh()`)

## Phase 7 — Evaluator rehearsal
- [ ] P0 Fresh clone runs from README
- [ ] P0 Replay, 10 concurrent receipts, kill worker mid-hang + restart, two workers

## Phase 8 — Docs & submission
- [ ] P0 README.md
- [ ] P0 AGENT_NOTES.md
- [ ] P0 Recording script (≤ 3 min)
- [ ] P0 Repo public, recording uploaded, links sent (Himanshu)

## Phase 9 — Stretch
- [ ] P2 `POST /ask` with read-only tools and logged tool calls
