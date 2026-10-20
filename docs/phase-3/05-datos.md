# F3 · 05 — Datos: OpenSearch, PostgreSQL y Redis

## 1. OpenSearch: índices y mappings

**[PP-F3-DAT-001] (MUST)** Versión OpenSearch 2.x (gestionado en prod). Mappings en `backend/internal/search/mappings/*.json` con `_meta.version`; plugins: `analysis-icu`.

Configuración común:

```jsonc
"settings": {
  "index": { "number_of_shards": 1, "number_of_replicas": 1, "refresh_interval": "1s" },
  "analysis": {
    "filter": {
      "es_stop": { "type": "stop", "stopwords": "_spanish_" },
      "es_stem": { "type": "stemmer", "language": "light_spanish" },
      "vet_synonyms": { "type": "synonym_graph", "synonyms_path": "analyzers/vet_synonyms_es.txt", "updateable": true },
      "edge": { "type": "edge_ngram", "min_gram": 2, "max_gram": 15 }
    },
    "analyzer": {
      "es_text":   { "tokenizer": "standard", "filter": ["lowercase", "icu_folding", "es_stop", "es_stem"] },
      "es_search": { "tokenizer": "standard", "filter": ["lowercase", "icu_folding", "vet_synonyms", "es_stop", "es_stem"] },
      "en_text":   { "tokenizer": "standard", "filter": ["lowercase", "icu_folding", "stop", "porter_stem"] },
      "suggest":   { "tokenizer": "standard", "filter": ["lowercase", "icu_folding", "edge"] },
      "suggest_q": { "tokenizer": "standard", "filter": ["lowercase", "icu_folding"] },
      "exact":     { "tokenizer": "keyword",  "filter": ["lowercase", "icu_folding"] }
    }
  }
}
```

`clinical` (campos principales):

```jsonc
"mappings": { "_meta": { "version": 1 }, "dynamic": "strict", "properties": {
  "doc_kind": { "type": "keyword" }, "pet_id": { "type": "keyword" }, "pet_name": { "type": "text", "analyzer": "es_text", "fields": { "exact": { "type": "text", "analyzer": "exact" } } },
  "species": { "type": "keyword" }, "organization_id": { "type": "keyword" }, "organization_name": { "type": "keyword" },
  "professional_id": { "type": "keyword" }, "occurred_at": { "type": "date" }, "encounter_type": { "type": "keyword" },
  "diagnoses": { "type": "nested", "properties": { "text": { "type": "text", "analyzer": "es_text", "search_analyzer": "es_search" }, "code": { "type": "keyword" }, "certainty": { "type": "keyword" } } },
  "text": { "type": "text", "analyzer": "es_text", "search_analyzer": "es_search", "term_vector": "with_positions_offsets" },
  "medications": { "properties": { "name": { "type": "text", "analyzer": "es_text" }, "catalog_id": { "type": "keyword" } } },
  "procedures": { "type": "text", "analyzer": "es_text" }, "vaccine_product": { "type": "text", "analyzer": "es_text" },
  "locale": { "type": "keyword" }, "year": { "type": "short" },
  "access": { "properties": { "guardian_id": { "type": "keyword" }, "org_ids_linked": { "type": "keyword" }, "org_ids_custody": { "type": "keyword" } } },
  "position": { "type": "long" } } }
```

`pets`: `name` (`es_text` + `suggest` subfield con `search_analyzer: suggest_q`), `guardian_display_name` (`text` + `suggest`), `microchip` (`keyword`), `microchip_suffix` (`keyword`), `breed` (`text`), `species/sex/status` (`keyword`), `birth_date` (`date`), `provisional` (`boolean`), `access`, `last_encounter_at`, `version` (`long`).

`medications`: `name` (`es_text` + `suggest`), `active_ingredients` (`text` + `suggest`), `synonyms` (`text`), `form/species/routes/status/atcvet_code/manufacturer` (`keyword`), `prescription_required/controlled` (`boolean`), `usage_count` (`integer`, usado en `function_score`).

`organizations`: `display_name` (`text` + `suggest`), `city/region/country/type/status` (`keyword`), `services` (`keyword`), `geo` (`geo_point`), `listed` (`boolean`).

Sinónimos (`vet_synonyms_es.txt`, versionado, ≈100 líneas iniciales): `vómito, emesis`, `picazón, prurito`, `diarrea, heces blandas`, `castrado, esterilizado, orquiectomía`, `garrapata, ixódido`, `rabia, antirrábica`… revisado por veterinario; recarga con `_reload_search_analyzers`.

## 2. PostgreSQL

**[PP-F3-DAT-002] (MUST)** Catálogo de medicamentos:

```sql
CREATE TABLE ref.medications (
  id uuid PRIMARY KEY, name text NOT NULL, name_norm text NOT NULL,
  active_ingredients jsonb NOT NULL DEFAULT '[]', form text NOT NULL, concentration text, presentation text,
  routes text[] NOT NULL DEFAULT '{}', species text[] NOT NULL DEFAULT '{}', manufacturer text, manufacturer_norm text,
  atcvet_code text, regulatory jsonb, controlled boolean NOT NULL DEFAULT false, prescription_required boolean NOT NULL DEFAULT true,
  default_dose_unit text, default_route text, synonyms text[] NOT NULL DEFAULT '{}',
  status text NOT NULL CHECK (status IN ('active','discontinued','merged')), merged_into uuid REFERENCES ref.medications(id),
  source jsonb NOT NULL, usage_count int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX medications_identity_uq ON ref.medications (name_norm, coalesce(manufacturer_norm,''), form, coalesce(concentration,'')) WHERE status <> 'merged';
CREATE INDEX medications_name_trgm ON ref.medications USING gin (name_norm gin_trgm_ops);
CREATE INDEX medications_species_idx ON ref.medications USING gin (species);

CREATE TABLE ref.medication_suggestions (
  id uuid PRIMARY KEY, proposed_by uuid NOT NULL, organization_id uuid NOT NULL, name text NOT NULL, notes text, photo_object_key text,
  status text NOT NULL CHECK (status IN ('pending','approved','rejected')), reviewed_by uuid, reason text, medication_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(), reviewed_at timestamptz
);
CREATE TABLE ref.medication_import_batches (
  id uuid PRIMARY KEY, source text NOT NULL, status text NOT NULL, dry_run boolean NOT NULL, created int, updated int, skipped int, errors jsonb,
  created_by uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), reverted_at timestamptz
);
CREATE TABLE ref.medication_import_rows (batch_id uuid NOT NULL, medication_id uuid NOT NULL, action text NOT NULL, previous jsonb, PRIMARY KEY (batch_id, medication_id));
CREATE TABLE ref.medications_changes (   -- cola de reindexación para CRUD (no ES)
  seq bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, medication_id uuid NOT NULL, changed_at timestamptz NOT NULL DEFAULT now()
);
```

**[PP-F3-DAT-003] (MUST)** Leases, cola de indexación y checkpoints de búsqueda:

```sql
CREATE TABLE platform.worker_leases (
  name text PRIMARY KEY, holder text NOT NULL, acquired_at timestamptz NOT NULL, expires_at timestamptz NOT NULL
);
CREATE TABLE search.index_queue (
  index text NOT NULL, doc_id text NOT NULL, pet_id uuid, version bigint NOT NULL,
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','queued','failed')), attempts int NOT NULL DEFAULT 0, last_error text,
  updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (index, doc_id)
);
CREATE INDEX index_queue_pending_idx ON search.index_queue (index, updated_at) WHERE status = 'pending';
CREATE TABLE search.reindex_jobs (id uuid PRIMARY KEY, alias text NOT NULL, from_index text, to_index text NOT NULL, status text NOT NULL, progress jsonb, started_by uuid, reason text, started_at timestamptz, finished_at timestamptz);
-- checkpoints: filas 'search.pets', 'search.clinical', 'search.medications', 'search.organizations' en platform.projection_checkpoints
```

**[PP-F3-DAT-005] (SHOULD)** Pacientes provisionales:

```sql
ALTER TABLE read_petregistry.pets ADD COLUMN provisional boolean NOT NULL DEFAULT false, ADD COLUMN provisional_org_id uuid, ADD COLUMN merged_into uuid;
ALTER TABLE read_petregistry.pets ALTER COLUMN guardian_id DROP NOT NULL;
CREATE TABLE read_petregistry.pet_claims (
  token_hash bytea PRIMARY KEY, pet_id uuid NOT NULL, organization_id uuid NOT NULL, contact_hash bytea NOT NULL, contact_kind text NOT NULL,
  status text NOT NULL CHECK (status IN ('pending','accepted','expired','cancelled')), expires_at timestamptz NOT NULL, resend_count int NOT NULL DEFAULT 0, created_at timestamptz NOT NULL
);
CREATE TABLE read_petregistry.pet_aliases (old_pet_id uuid PRIMARY KEY, pet_id uuid NOT NULL, merged_at timestamptz NOT NULL);
```

**[PP-F3-DAT-006] (SHOULD)** Push:

```sql
CREATE TABLE notifications.push_subscriptions (
  id uuid PRIMARY KEY, user_id uuid NOT NULL, endpoint_hash bytea NOT NULL UNIQUE, endpoint text NOT NULL, p256dh text NOT NULL, auth text NOT NULL,
  user_agent_hash bytea, status text NOT NULL CHECK (status IN ('active','expired')), created_at timestamptz NOT NULL DEFAULT now(), last_seen_at timestamptz
);
ALTER TABLE notifications.preferences ADD COLUMN push jsonb NOT NULL DEFAULT '{"appointments":true,"preventive_care":true,"clinical":false,"account":false,"security":true}';
ALTER TABLE notifications.deliveries DROP CONSTRAINT deliveries_channel_check, ADD CONSTRAINT deliveries_channel_check CHECK (channel IN ('email','push'));
```

**[PP-F3-DAT-007] (MUST)** Revisión de `es.events` y de índices:
- Medir con datos sintéticos (PRF-002): tamaño de `es.events`, bloat, tiempo de `ReadAll` filtrado por tipo. Si > 20 M filas o el índice `events_pos_type_idx` > 2 GB → particionado declarativo por rango de `global_position` (100 M por partición) con migración *online* (tabla nueva particionada + copia por lotes + swap), decisión documentada en una nota de tracking.
- Añadir `es.events.xid xid8 DEFAULT pg_current_xact_id()` **solo** si se activa ARQ-011.
- Auditoría de índices no usados (`pg_stat_user_indexes`) y de consultas lentas (`pg_stat_statements`, `auto_explain` > 200 ms) con informe en PR (PRF-004).

**[PP-F3-DAT-008] (SHOULD)** Vistas materializadas de plataforma: `platform.stats_daily` (`day, metrics jsonb`) refrescada por job `stats:refresh` cada hora (`REFRESH MATERIALIZED VIEW CONCURRENTLY`); fuentes: `read_*`, `notifications.deliveries`, `platform.projection_checkpoints`. Sin PII.

**[PP-F3-DAT-009] (MUST)** Backups y retención de OpenSearch: *snapshot* diario a object storage (`_snapshot` repositorio S3), retención 7 días; **no es necesario para recuperación** (rebuild desde `es.events`), solo para acelerar la restauración. Redis: AOF `everysec` en la base de colas; sin backup (las fuentes están en PG).

## 3. Redis: esquema de claves

**[PP-F3-DAT-004] (MUST)** Prefijo `pp:v1:`; familias y TTL en ARQ-006/007. Reglas: toda clave tiene TTL (salvo asynq); valores ≤ 64 KB; sin PII de contacto; `OBJECT ENCODING` revisado para listas grandes; memoria objetivo caché ≤ 512 MB en prod (alerta a 80%). Claves de asynq gestionadas por la librería (`asynq:{...}`) en la base `jobs`. Pub/Sub canal `pp:cache:invalidate`.
