-- +goose Up
-- Cobro real, dunning y facturación (PP-F5-ARQ-002, ARQ-003, ARQ-006, SEC-002).

-- Un intento de cobro por (cargo, intento): la referencia que viaja al proveedor
-- es "<charge_id>-<attempt>" y es la clave de idempotencia del cobro.
CREATE TABLE billing.payment_attempts (
  charge_id uuid NOT NULL,
  attempt int NOT NULL CHECK (attempt >= 1),
  provider text NOT NULL,
  provider_ref text,
  status text NOT NULL CHECK (status IN ('created','pending','paid','failed','voided')),
  error_code text,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (charge_id, attempt)
);
-- Como mucho un intento abierto por cargo: impide cobrar dos veces mientras el
-- proveedor aún no ha resuelto el anterior.
CREATE UNIQUE INDEX payment_attempts_open_idx ON billing.payment_attempts (charge_id)
  WHERE status IN ('created','pending');
CREATE INDEX payment_attempts_ref_idx ON billing.payment_attempts (provider, provider_ref)
  WHERE provider_ref IS NOT NULL;

-- El calendario de dunning se ancla al primer fallo del cargo (ARQ-006).
ALTER TABLE billing.charges ADD COLUMN first_failed_at timestamptz;
ALTER TABLE billing.charges ADD COLUMN payment_ref text;
ALTER TABLE billing.charges ADD COLUMN provider text;

ALTER TABLE billing.payments ADD COLUMN account_id uuid;
ALTER TABLE billing.payments ADD COLUMN currency char(3);
ALTER TABLE billing.payments ADD COLUMN reason text;
CREATE INDEX payments_account_idx ON billing.payments (account_id, at DESC);
CREATE INDEX payments_charge_idx ON billing.payments (charge_id);

-- Una discrepancia abierta por (cargo, tipo): la conciliación diaria es idempotente.
CREATE UNIQUE INDEX discrepancies_open_idx ON billing.discrepancies (charge_id, kind)
  WHERE status = 'open' AND charge_id IS NOT NULL;
CREATE INDEX discrepancies_status_idx ON billing.discrepancies (status, detected_at DESC);

ALTER TABLE billing.invoices ADD COLUMN contingency_at timestamptz;
ALTER TABLE billing.invoices ADD COLUMN next_attempt_at timestamptz;
CREATE INDEX invoices_account_idx ON billing.invoices (account_id, created_at DESC);
CREATE INDEX invoices_status_idx ON billing.invoices (status);

ALTER TABLE billing.credit_notes ADD COLUMN refund_ref text;
CREATE UNIQUE INDEX credit_notes_refund_idx ON billing.credit_notes (invoice_id, refund_ref)
  WHERE refund_ref IS NOT NULL;

GRANT SELECT, INSERT, UPDATE, DELETE ON billing.payment_attempts TO pp_app_writer;
GRANT SELECT ON billing.payment_attempts TO pp_app_reader;

-- +goose Down
DROP INDEX IF EXISTS billing.credit_notes_refund_idx;
ALTER TABLE billing.credit_notes DROP COLUMN IF EXISTS refund_ref;
DROP INDEX IF EXISTS billing.invoices_status_idx;
DROP INDEX IF EXISTS billing.invoices_account_idx;
ALTER TABLE billing.invoices DROP COLUMN IF EXISTS next_attempt_at;
ALTER TABLE billing.invoices DROP COLUMN IF EXISTS contingency_at;
DROP INDEX IF EXISTS billing.discrepancies_status_idx;
DROP INDEX IF EXISTS billing.discrepancies_open_idx;
DROP INDEX IF EXISTS billing.payments_charge_idx;
DROP INDEX IF EXISTS billing.payments_account_idx;
ALTER TABLE billing.payments DROP COLUMN IF EXISTS reason;
ALTER TABLE billing.payments DROP COLUMN IF EXISTS currency;
ALTER TABLE billing.payments DROP COLUMN IF EXISTS account_id;
ALTER TABLE billing.charges DROP COLUMN IF EXISTS provider;
ALTER TABLE billing.charges DROP COLUMN IF EXISTS payment_ref;
ALTER TABLE billing.charges DROP COLUMN IF EXISTS first_failed_at;
DROP TABLE IF EXISTS billing.payment_attempts;
