-- 0001_init: job-work issues and receipts, and the accounting vouchers they produce.

CREATE TABLE skus (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code      TEXT NOT NULL UNIQUE,
    name      TEXT NOT NULL,
    image_url TEXT NOT NULL
);

-- Job-workers.
CREATE TABLE parties (
    id    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name  TEXT NOT NULL,
    phone TEXT NOT NULL
);

-- Added to the brief's design:
--   rate_paise   agreed job-work charge per piece; a receipt's voucher is qty * rate_paise.
--   received_qty running total of receipts, updated in the same transaction as each receipt.
--                The CHECK makes receiving more than was issued impossible in the database.
CREATE TABLE jobwork_issues (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    party_id     BIGINT NOT NULL REFERENCES parties (id),
    sku_id       BIGINT NOT NULL REFERENCES skus (id),
    qty          INTEGER NOT NULL CHECK (qty > 0),
    rate_paise   BIGINT NOT NULL CHECK (rate_paise >= 0),
    received_qty INTEGER NOT NULL DEFAULT 0,
    issued_on    DATE NOT NULL,
    CONSTRAINT received_within_issued CHECK (received_qty BETWEEN 0 AND qty)
);

CREATE TABLE jobwork_receipts (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    issue_id        BIGINT NOT NULL REFERENCES jobwork_issues (id),
    qty             INTEGER NOT NULL CHECK (qty > 0),
    received_on     DATE NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX jobwork_receipts_issue_id_idx ON jobwork_receipts (issue_id);

-- Added to the brief's design, for the sync worker:
--   next_attempt_at  when the voucher may be pushed again (backoff).
--   locked_by/until  lease held by the worker pushing it; an expired lease means that worker died.
-- client_ref is generated once and sent on every attempt, so the accounting side can dedupe.
CREATE TABLE vouchers (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    type            TEXT NOT NULL CHECK (type IN ('jobwork_receipt')),
    source_id       BIGINT NOT NULL,
    amount_paise    BIGINT NOT NULL CHECK (amount_paise >= 0),
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'pushed', 'failed')),
    attempts        INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error      TEXT,
    client_ref      UUID NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    external_ref    TEXT,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_by       TEXT,
    locked_until    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT one_voucher_per_source UNIQUE (type, source_id),
    CONSTRAINT pushed_has_external_ref CHECK (status <> 'pushed' OR external_ref IS NOT NULL)
);

CREATE INDEX vouchers_pending_due_idx ON vouchers (next_attempt_at) WHERE status = 'pending';
CREATE INDEX vouchers_status_id_idx ON vouchers (status, id);
