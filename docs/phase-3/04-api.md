# F3 · 04 — API REST v1 (ampliación)

Convenciones de F1/F2 vigentes. Toda respuesta de búsqueda incluye `freshness{lag_seconds}` y la cabecera `X-PP-Search-Mode: full|degraded`.

## 1. Búsqueda — **[PP-F3-API-001] (MUST)**

| Método y ruta | Authz | Parámetros / respuesta |
|---|---|---|
| `GET /organizations/{org_id}/search/patients?q=&species=&status=&cursor&size` | `search:patients` (miembro) | `{data: [{pet{id, name, species, breed, sex, age, photo_thumb_url?, microchip_suffix, status}, guardian_display_name, access_level linked\|custody, last_encounter_at, highlight{…}}], facets{species[]}, next_cursor, freshness, mode}` |
| `GET /organizations/{org_id}/search/clinical?q=&species=&type=&professional_id=&from=&to=&confirmed_only=&organization_scope=all\|mine&cursor&size` | `search:clinical` (miembro) | `{data: [{pet{…}, hits: [{doc_kind, ref_id, occurred_at, organization{id, name}, encounter_type, diagnoses[], highlight{text[], diagnoses[]}}]}], facets{species, encounter_type, organization, year}, next_cursor, freshness, mode}` |
| `GET /me/search?q=&pet_id=&kind=&from=&to=&cursor&size` | autenticado (tutor) | igual, limitado a mis mascotas |
| `GET /admin/search/clinical?q=&reason=` | `super_admin` + motivo | auditado; sin filtro de acceso |

Reglas: `q` 1–200 caracteres; `size` ≤ 50; filtros validados por esquema; `organization_scope=mine` restringe a documentos de la propia org (custodia incluida).

## 2. Autocompletado — **[PP-F3-API-002] (MUST)**

| Método y ruta | Authz | Respuesta |
|---|---|---|
| `GET /organizations/{org_id}/suggest/patients?q=` | `search:patients` | `{data: [{pet_id, name, species, guardian_display_name, microchip_suffix, access_level}]}` (≤ 8, 200 ms) |
| `GET /suggest/medications?q=&species=&limit=` | miembro clínico | `{data: [{catalog_id, name, active_ingredients[], form, concentration, presentation, manufacturer, prescription_required, controlled, default_route?, default_dose_unit?}]}` |
| `GET /me/suggest/pets?q=` | tutor | mis mascotas |

## 3. Catálogo de medicamentos — **[PP-F3-API-003] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /catalogs/medications?q=&species=&form=&prescription_required=&status=&cursor` | miembro clínico | listado (PG para filtros exactos; `q` → OpenSearch) |
| `GET /catalogs/medications/{id}` | miembro clínico | `Medication` (si `merged`, `301` lógica: devuelve el destino con `merged_from`) |
| `POST /catalogs/medications/suggestions` *(Idempotency-Key)* | `veterinarian` | `{name, notes?, photo_object_key?}` → `201 Suggestion` |
| `GET /organizations/{org_id}/catalogs/medications/suggestions` | miembro | mis sugerencias y estado |
| `POST /admin/catalogs/medications` / `PUT /{id}` / `POST /{id}/discontinue` / `POST /{id}/merge` | `catalog:admin` | con `{reason}`; `merge{into}` |
| `POST /admin/catalogs/medications/import` | `catalog:admin` | multipart CSV/JSON + `{source, dry_run}` → `202 {batch_id}`; `GET /admin/catalogs/medications/imports/{batch_id}` → `{status, created, updated, skipped, errors[]}`; `POST …/revert` |
| `GET /admin/catalogs/medications/suggestions?status=` / `POST /{id}/approve` / `/reject` | `catalog:admin` | `{reason}` |
| `GET /admin/catalogs/medications/usage?from=&to=` | `catalog:admin` | `{items_total, with_catalog_id, ratio, top[]}` |

## 4. Cohortes — **[PP-F3-API-004] (SHOULD)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /organizations/{org_id}/cohorts` | `cohort:run` (admin, vet) | definiciones disponibles con esquema de parámetros |
| `POST /organizations/{org_id}/cohorts/{key}/run` | `cohort:run` | `{params}` → `200 {data: CohortRow[], total, truncated}` (≤ 1.000) |
| `POST /organizations/{org_id}/cohorts/{key}/export` | `cohort:export` (admin) + `{reason}` | → `202 {job_id}`; CSV con contacto, auditado (RN-20) |
| `POST /organizations/{org_id}/cohorts/{key}/notify` *(Idempotency-Key)* | `cohort:notify` (admin) | `{params, message ≤ 500}` → `202 {scheduled: n, skipped_by_preference: m}`; máx 1 campaña / cohorte / 7 días |

## 5. Directorio — **[PP-F3-API-005] (SHOULD)**

| Método y ruta | Authz | Respuesta |
|---|---|---|
| `GET /directory/organizations?q=&city=&country=&near=lat,lon&radius_km=&cursor` | público (rate limit `public`) | `{data: [{id, display_name, city, neighborhood?, services[], phone? , distance_km?}]}` sin dirección exacta |
| `GET /directory/organizations/{id}` | público | ficha pública mínima |
| `PUT /organizations/{org_id}/directory-listing` *(If-Match)* | `org:update` | `{listed, services[], show_phone, neighborhood?, geo?}` |
| `POST /pets/{pet_id}/care-links` con `{organization_id}` | (F1) | vincular desde el directorio reutiliza el endpoint existente |

## 6. Panel de plataforma — **[PP-F3-API-006] (SHOULD)**

`GET /admin/platform/stats?granularity=day|week|month&from=&to=` → series `{users_active, users_new, organizations{by_status}, pets, encounters_signed, appointments{…}, vaccinations, notifications{sent, failed}, search{queries, degraded}, projections{max_lag_seconds, failed[]}}`; `GET /admin/platform/top-organizations?metric=&period=`.

## 7. Pacientes provisionales — **[PP-F3-API-007] (SHOULD)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `POST /organizations/{org_id}/provisional-pets` *(Idempotency-Key)* | `pet:register_provisional` | `{pet{name, species, …}, guardian_contact{email?\|phone?}, send_invitation: bool}` → `201 Pet{provisional: true, claim{status, expires_at}}` |
| `POST /organizations/{org_id}/provisional-pets/{pet_id}/resend-claim` | ídem | → `204` (máx 3 / 7 días) |
| `GET /pet-claims/{token}` | autenticado | previsualización `{pet{name, species}, organization{display_name}, contact_masked, existing_match?{pet_id, name}}` |
| `POST /pet-claims/{token}/accept` | autenticado; contacto verificado coincide | `{merge_into_pet_id?}` → `200 Pet` |
| `GET /organizations/{org_id}/provisional-pets?status=unclaimed\|claimed\|expired` | miembro | listado |

## 8. Push — **[PP-F3-API-008] (SHOULD)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /push/vapid-public-key` | autenticado | `{key}` |
| `POST /me/push-subscriptions` | `me:update` | `{endpoint, keys{p256dh, auth}, user_agent?}` → `201 {id}` (upsert por endpoint) |
| `GET /me/push-subscriptions` / `DELETE /me/push-subscriptions/{id}` | `me:update` | |
| `PUT /me/notification-preferences` (F2) | | ahora `{email{…}, push{…}, digest{…}}` |

## 9. Administración de búsqueda y caché — **[PP-F3-API-009] (MUST)**

| Método y ruta | Authz | Respuesta |
|---|---|---|
| `GET /admin/search/indices` | `super_admin` | `[{alias, physical, docs, size_bytes, mapping_version, repo_mapping_version, drift: bool, lag_seconds}]` |
| `POST /admin/search/indices/{alias}/reindex` | `super_admin` + motivo | `202 {job_id}`; progreso en `GET /admin/search/reindex/{job_id}` |
| `POST /admin/search/index-queue/retry` | `super_admin` | re-encola `failed` |
| `POST /admin/cache/purge` | `super_admin` + motivo | `{families: ["membership", …]}` → `202` |
| `GET /admin/workers` | `super_admin` | `{instances[], leases[{name, holder, expires_at}], asynq{queues{size, processed, failed}}}` |

## 10. Límites y degradación — **[PP-F3-API-010] (MUST)**

- `429 RATE_LIMITED` con `Retry-After` y `RateLimit-*` (ARQ-007); `detail` humano con el grupo de rutas.
- `X-PP-Search-Mode: degraded` + `freshness` cuando aplica fallback; `503 SEARCH_UNAVAILABLE` solo si tampoco PG puede atender (muy improbable).
- Cabecera `X-PP-Freshness` extendida a resultados de búsqueda.

## 11. Catálogo de errores (ampliación) — **[PP-F3-API-011] (MUST)**

| `code` | HTTP | Cuándo |
|---|---|---|
| `SEARCH_UNAVAILABLE` | 503 | Sin OpenSearch ni fallback. |
| `SEARCH_QUERY_INVALID` | 422 | `q` vacío/largo, filtros inválidos. |
| `MEDICATION_NOT_ACTIVE` | 422 | `catalog_id` discontinuado o inexistente en `SetPlan`. |
| `MEDICATION_DUPLICATE` | 409 | Alta con nombre/forma/concentración existentes. |
| `MERGE_CHAIN_TOO_DEEP` | 409 | |
| `IMPORT_BATCH_INVALID` | 422 | Errores de validación en lote (`errors[]`). |
| `COHORT_TOO_LARGE` | 422 | > 1.000 filas en exportación; refinar parámetros. |
| `CAMPAIGN_RATE_LIMITED` | 429 | Campaña reciente sobre la misma cohorte. |
| `DIRECTORY_NOT_LISTED` | 404 | Organización no listada. |
| `CLAIM_CONTACT_MISMATCH` | 403 | Contacto verificado no coincide. |
| `CLAIM_EXPIRED` | 410 | |
| `PROVISIONAL_ONLY_CREATOR` | 403 | Edición por otra org. |
| `PUSH_SUBSCRIPTION_INVALID` | 422 | |
| `ATTACHMENT_SCAN_PENDING` / `ATTACHMENT_QUARANTINED` | 409 / 410 | Descarga antes de escaneo / infectado. |
| `ADMIN_REASON_REQUIRED` | 422 | Operación admin sin motivo. |
