-- +goose Up
-- F4 M4.1: Tablas de OCR, uso de IA, presupuesto, métricas y evaluación.
-- Requisitos: PP-F4-DAT-004, PP-F4-OCR-008, PP-F4-OCR-010.

CREATE SCHEMA IF NOT EXISTS ocr;

GRANT USAGE ON SCHEMA ocr TO pp_app_writer, pp_app_reader;
ALTER DEFAULT PRIVILEGES IN SCHEMA ocr GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pp_app_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA ocr GRANT SELECT ON TABLES TO pp_app_reader;

-- 1. Uso diario de IA (PP-F4-DAT-004, PP-F4-OCR-008)
CREATE TABLE IF NOT EXISTS ocr.ai_usage_daily (
  day date NOT NULL,
  provider text NOT NULL,
  model text NOT NULL,
  requests int NOT NULL DEFAULT 0,
  input_tokens bigint NOT NULL DEFAULT 0,
  output_tokens bigint NOT NULL DEFAULT 0,
  cost_usd numeric(12,4) NOT NULL DEFAULT 0.0000,
  refusals int NOT NULL DEFAULT 0,
  PRIMARY KEY (day, provider, model)
);

-- 2. Presupuesto mensual de IA con alerta y corte duro (PP-F4-DAT-004, PP-F4-OCR-008)
CREATE TABLE IF NOT EXISTS ocr.budget (
  id smallint PRIMARY KEY DEFAULT 1,
  monthly_usd numeric(12,2) NOT NULL DEFAULT 300.00,
  hard_stop boolean NOT NULL DEFAULT true,
  alert_pct smallint NOT NULL DEFAULT 80,
  updated_by uuid REFERENCES identity.user_accounts(id),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT single_row CHECK (id = 1)
);

INSERT INTO ocr.budget (id, monthly_usd, hard_stop, alert_pct)
VALUES (1, 300.00, true, 80)
ON CONFLICT (id) DO NOTHING;

-- 3. Métricas diarias agregadas de OCR y tasas de edición (PP-F4-DAT-004, PP-F4-OCR-010)
CREATE TABLE IF NOT EXISTS ocr.metrics_daily (
  day date PRIMARY KEY,
  volume int NOT NULL DEFAULT 0,
  done int NOT NULL DEFAULT 0,
  reupload int NOT NULL DEFAULT 0,
  failed int NOT NULL DEFAULT 0,
  refused int NOT NULL DEFAULT 0,
  avg_latency_ms int NOT NULL DEFAULT 0,
  field_edit_rates jsonb NOT NULL DEFAULT '{}',
  items_total int NOT NULL DEFAULT 0,
  items_accepted int NOT NULL DEFAULT 0,
  items_edited int NOT NULL DEFAULT 0,
  items_removed int NOT NULL DEFAULT 0,
  items_added int NOT NULL DEFAULT 0
);

-- 4. Dataset de evaluación anonimizado (PP-F4-DAT-004, PP-F4-OCR-007, PP-F4-SEC-008)
CREATE TABLE IF NOT EXISTS ocr.eval_samples (
  id uuid PRIMARY KEY,
  object_key text NOT NULL,
  labels jsonb NOT NULL,
  source text NOT NULL CHECK (source IN ('synthetic','donated','production_consented')),
  consent_ref text,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- 5. Ejecuciones históricas de evaluación (PP-F4-DAT-004, PP-F4-OCR-007, PP-F4-QA-003)
CREATE TABLE IF NOT EXISTS ocr.eval_runs (
  id uuid PRIMARY KEY,
  extractor_id text NOT NULL,
  dataset_version text NOT NULL,
  metrics jsonb NOT NULL,
  cost_usd numeric(10,4),
  ran_at timestamptz NOT NULL DEFAULT now(),
  git_sha text
);

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ocr TO pp_app_writer;
GRANT SELECT ON ALL TABLES IN SCHEMA ocr TO pp_app_reader;

-- +goose Down
DROP SCHEMA IF EXISTS ocr CASCADE;
