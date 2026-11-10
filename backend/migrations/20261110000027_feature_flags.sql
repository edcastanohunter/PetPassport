-- 20261110000027_feature_flags.sql: Feature flags for progressive delivery (PP-F5-OPS-010).

CREATE TABLE IF NOT EXISTS platform.feature_flags (
    key text PRIMARY KEY,
    enabled boolean NOT NULL DEFAULT false,
    rollout_percentage integer NOT NULL DEFAULT 100 CHECK (rollout_percentage >= 0 AND rollout_percentage <= 100),
    targeting jsonb NOT NULL DEFAULT '{}'::jsonb,
    description text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Seed initial phase 5 flags
INSERT INTO platform.feature_flags (key, enabled, rollout_percentage, description)
VALUES 
    ('f5_billing_portal', true, 100, 'Acceso al portal y ajustes de facturación'),
    ('f5_canary_routing', false, 10, 'Enrutamiento canary para nuevas versiones'),
    ('f5_sms_notifications', false, 0, 'Canal SMS / WhatsApp en notificaciones'),
    ('f5_service_status_banner', true, 100, 'Banner global de estado del servicio')
ON CONFLICT (key) DO NOTHING;

GRANT SELECT ON platform.feature_flags TO pp_api;
GRANT SELECT, INSERT, UPDATE ON platform.feature_flags TO pp_admin;
