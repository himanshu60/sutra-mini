# Agent notes

## Tools

- **Claude Code** (Claude Opus 5.5) in VS Code on Windows 10 wrote the plan, the Go and TypeScript code, the
  tests and these docs, and ran every command.
- **My part:**
  - I gave it the brief and a long prompt that required a plan before any code, P0/P1/P2 TODOs with acceptance
    checks, and tests that must fail when their guarantee is broken.
  - I reviewed and approved the plan.
  - I decided where tests run (a local Postgres) and that every verified phase gets its own commit.
- **Stack:** Go 1.27 toolchain (the module targets 1.25), pgx v5, godotenv, Next.js 16.4, Neon for the app
  database, and PostgreSQL 17.11 locally for the tests.

## How the work went

Plan first ([TODO.md](TODO.md)), then one phase at a time. After every file: `gofmt`/`go vet` or
`tsc`/`eslint`. After every phase: tests, a commit, and a live run of the real binaries. The two mandatory tests
and the worker tests were then **mutation-checked**: the guarantee was deliberately broken to confirm the test
goes red, and the files were restored byte for byte.

## What the agent got wrong, and how it surfaced

1. **The overshoot backstop answered 500.** The plan said a violation of the `received_within_issued` CHECK
   would map to `409`, but the first implementation left that out. The mutation check caught it: with only the
   `WHERE` guard removed, the CHECK still prevented overshoot, but 7 of 10 requests got HTTP 500. Fixed by
   rolling back and returning `409` with the outstanding quantity. With the guard removed the test now passes
   (the backstop alone holds). With the guard and the CHECK both removed it fails:
   *"OVERSHOOT: 30 received against 10 issued"*.
2. **The mock reused external refs across restarts.** In the end-to-end rehearsal the database showed 28 pushed
   vouchers but only 20 distinct `external_ref`s, because the mock restarted its numbering at `ACC-000001` every
   run. Nothing was double-posted, but on the dashboard it looked exactly like double posting. Fixed with a
   random per-run tag (`ACC-7KQ2-000001`). The timeout test now asserts the exact ref the mock recorded instead
   of a hard-coded string.
3. **The integration tests skipped silently.** With no `TEST_DATABASE_URL`, `go test ./...` printed `ok` for
   packages whose tests never ran. Spotted while writing the README; they now fail with an explicit message.
4. **The API logged "listening" before it had the port.** This came out of chasing an "API unreachable" message
   on `/sync`. That message was most likely a race in the agent's own test script (it started the API and
   Next.js together; a re-run that waited for the API rendered correctly 3 times out of 3), but the log line
   was misleading either way: it printed before binding. Fixed with
   `net.Listen` and then `Serve`.
5. **Out-of-date framework knowledge.** The Next.js scaffold shipped an `AGENTS.md` warning that version 16
   differs from what models were trained on. The agent read the bundled docs instead of guessing:
   - With `cacheComponents` on, an uncached fetch must sit inside `<Suspense>`.
   - A Server Action refreshes the page with `refresh()` from `next/cache`.

   It also noticed that the generated `web/.gitignore` (`.env*`) would have silently left `web/.env.example`
   out of the repo.
6. **Its own shell slips**, minor but real:
   - A `wait` in a shell that also had the servers in the background hung the session.
   - A throwaway `cp -r .` copied `node_modules` and `.env` into a temp folder, which was deleted.
   - The first attempt to install PostgreSQL via winget failed: EnterpriseDB's download server returned 403.
     The official 17.11 Windows binaries were taken from Maven Central instead (SHA-1 verified) and started
     with `initdb` + `pg_ctl` on localhost only.

## One thing I caught

The catches above came out of checks I insisted on in the prompt rather than from reading diffs line by line.
"Tests must fail if the guarantee is broken" became the mutation check that exposed #1, and "design for the
evaluator's script" became the full rehearsal that exposed #2.

## Trade-offs I would defend in review

- **Running total + CHECK, not `SELECT … FOR UPDATE` + `SUM`.** The database itself refuses an overshoot, and a
  single conditional `UPDATE` is the whole concurrency story. The cost is a denormalised counter. It changes only
  in the same transaction as each receipt, and Test A asserts it equals the sum of receipts.
- **Lease column, not a row lock held across the HTTP call.** Holding a transaction open for up to 8 s per
  voucher ties up pooled connections. A persisted lease is also visible on the dashboard ("pushing (worker …)").
  `SKIP LOCKED` covers the moment of claiming; the lease covers the push.
- **Attempts are counted when a voucher is claimed,** so a voucher whose push keeps crashing the worker still
  ends `failed` instead of looping forever.
- **Idempotency stores successes only, and the key is optional.** A rejected request leaves nothing behind. A
  missing key is generated and echoed back, so the caller can still replay.
- **`client_ref` is a random UUID, not derived from the voucher id.** An id-based ref would collide after a
  database reset and could pick up another voucher's 409.
- **Delivery is at-least-once with remote dedupe**, not "exactly once". The window between the accounting
  system accepting a voucher and our database recording it cannot be closed from our side. The design makes
  that window safe instead of pretending it isn't there.

## Not done

- The `POST /ask` stretch goal.
- Running `go test -race` (there is no 64-bit C compiler on this Windows machine).
- Limitations are listed in the [README](README.md#assumptions-and-limitations).
