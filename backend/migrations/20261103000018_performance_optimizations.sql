-- +goose Up
-- PP-F3-PRF-004 / PP-F3-DAT-007: Optimización PostgreSQL, índices para cursores y fillfactor

-- Índice compuesto para listados de mascotas con paginación por cursor determinista
CREATE INDEX IF NOT EXISTS pets_guardian_cursor_idx
  ON read_petregistry.pets (guardian_id, status, created_at DESC, id);

-- Fillfactor 90 para tablas con alta tasa de UPDATEs frecuentes (reduce bloat y HOT updates)
ALTER TABLE read_scheduling.appointments SET (fillfactor = 90);
ALTER TABLE platform.projection_checkpoints SET (fillfactor = 90);
ALTER TABLE platform.worker_leases SET (fillfactor = 90);

-- +goose Down
ALTER TABLE platform.worker_leases RESET (fillfactor);
ALTER TABLE platform.projection_checkpoints RESET (fillfactor);
ALTER TABLE read_scheduling.appointments RESET (fillfactor);
DROP INDEX IF EXISTS read_petregistry.pets_guardian_cursor_idx;
