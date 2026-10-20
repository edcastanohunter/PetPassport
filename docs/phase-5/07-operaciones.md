# F5 · 07 — Operaciones: Kubernetes, continuidad, on-call, incidentes, status y costes

Marco: ADR-024. Área `OPS`.

## 1. Migración a Kubernetes

**[PP-F5-OPS-001] (MUST)**

- Clúster gestionado por entorno (staging primero, 4 semanas antes que prod), ≥ 2 zonas, *node pools* `general` (api/worker) y `jobs` (asynq pesados: OCR, PDF) con *taints*; Helm chart de F3 como fuente; `api` 3 réplicas (HPA CPU 60 %, máx 10), `worker` 2 réplicas (HPA por `projection_lag`/cola asynq vía KEDA, COULD), Traefik ingress con CRDs equivalentes a la configuración actual, cert-manager (o certificados del WAF), External Secrets, PDB (`minAvailable: 2` api, `1` worker), `topologySpreadConstraints` por zona, *requests/limits* medidos en F3 (PRF-008), `readiness/liveness` existentes, imágenes firmadas verificadas por política de admisión (Kyverno/Sigstore policy-controller), `NetworkPolicy` por defecto deny + reglas explícitas (api→PG/Redis/OpenSearch/Keycloak; worker→egress proxy).
- Plan de corte: réplica lógica PG VM→gestionado multi-AZ (ya gestionado desde F1: si el actual es single-AZ, promoción a multi-AZ es una operación del proveedor) → despliegue completo en K8s apuntando a la misma BD → pruebas → cambio de DNS en el WAF (TTL 60 s) → observación 24 h → apagado de la VM; rollback = DNS de vuelta.
- Criterio de éxito: cero pérdida de datos, corte < 15 min, SLOs cumplidos 7 días después.

## 2. Datos multi-AZ

**[PP-F5-OPS-002] (MUST)** PostgreSQL gestionado multi-AZ con réplica síncrona y *failover* automático (< 60 s), réplica de lectura para reportes/BI (COULD), *connection pooler* del proveedor o `pgbouncer` (F3 PRF-005 se activa aquí); Redis gestionado con réplica y *failover*; OpenSearch gestionado con 2 nodos en zonas distintas y réplica por índice; object storage con versionado y replicación cross-region.

## 3. Backups

**[PP-F5-OPS-003] (MUST)** PostgreSQL: snapshots diarios + WAL continuo (PITR) retención 30 días, copia a segunda región cifrada (clave distinta), retención 30 días; dump lógico semanal de `es.*` a object storage (JSONL) como respaldo independiente del proveedor; `privacy.subject_keys` con backup separado cifrado con KMS, **nunca** en el mismo artefacto que los datos (DAT-006); OpenSearch snapshots diarios (aceleran, no son necesarios); verificación automática de integridad de cada backup (restauración en entorno efímero + `SELECT count(*)` + `ReadAll` muestra) semanal con alerta; inventario de backups con fechas y tamaños en el panel ops.

## 4. Recuperación ante desastres

**[PP-F5-OPS-004] (MUST)** Runbooks por escenario con RTO/RPO objetivo y medido:

| Escenario | Respuesta | RTO objetivo | RPO objetivo |
|---|---|---|---|
| Fallo de zona | automático (K8s + failover PG) | < 15 min | 0 |
| Corrupción lógica (bug que escribe mal) | PITR a punto anterior + replay selectivo + rebuild de proyecciones | < 2 h | ≤ 5 min antes del incidente |
| Pérdida de región | restaurar desde copia cross-region en región alternativa, Helm, DNS | < 4 h | ≤ 5 min (WAL replicado) |
| Pérdida de claves de sujeto | restaurar backup separado de `subject_keys` + KMS (claves en KMS no se pierden) | < 1 h | 0 |
| Compromiso de credenciales de infraestructura | rotación total (runbook de seguridad F1) | < 4 h | — |

Simulacro **trimestral** de restauración completa (`RESTORE-LOG.md` con tiempos reales); anual de pérdida de región; cada runbook ejecutado por alguien distinto de su autor al menos una vez al año.

## 5. On-call

**[PP-F5-OPS-005] (MUST)** Rotación semanal (≥ 2 personas; primario/secundario), herramienta de guardias con escalado (15 min sin ack → secundario → responsable), horario 24×7 solo para alertas `page` (caída, error budget quemándose rápido, pagos/facturación fallando masivamente, pérdida de datos, seguridad); el resto `ticket` en horario laboral; cada alerta `page` tiene runbook enlazado; revisión mensual de alertas (ruido < 10 % de páginas sin acción); compensación/tiempo libre documentado.

## 6. Gestión de incidentes

**[PP-F5-OPS-006] (MUST)** Proceso: detección → declaración (severidad S1–S4 con definiciones) → canal de incidente + rol de *incident commander* + comunicador → actualizaciones en status page (S1/S2 cada 30 min) → mitigación → resolución → **postmortem sin culpa en ≤ 5 días hábiles** (línea temporal, impacto, causas, acciones con dueño y fecha, seguimiento hasta cierre); plantillas en `docs/ops/incident-template.md` y `postmortem-template.md`; registro de incidentes y métricas (MTTD, MTTR); comunicación a clientes afectados (S1/S2) por email y, si hay datos personales afectados, procedimiento de notificación legal (CMP-004).

## 7. Status page

**[PP-F5-OPS-007] (MUST)** Proveedor externo (hospedado fuera de nuestra infraestructura); componentes: Aplicación web, API, API de partners, Autenticación, Notificaciones, Integraciones de pago; *checks* externos cada minuto desde 3 regiones; incidentes y mantenimientos (aviso ≥ 72 h para programados con impacto); suscripción por email/RSS; `status.json` público consumido por la SPA; historial de disponibilidad 90 días visible.

## 8. SLA y error budget

**[PP-F5-OPS-008] (MUST)** SLOs de F3 vigentes; disponibilidad contractual por plan (Pro 99,5 %, Enterprise 99,9 %) medida como `1 − (minutos con tasa 5xx > 5 % o indisponibilidad total) / minutos del mes`, excluyendo mantenimientos programados anunciados; créditos automáticos (Enterprise: 10 % < 99,9 %, 25 % < 99 %, 50 % < 95 %) aplicados como `AdminAdjustment{credit}` en la siguiente factura; política de error budget: si el presupuesto mensual se agota, congelación de despliegues de funcionalidad hasta recuperar (solo fiabilidad), decisión registrada.

## 9. Capacidad y costes (FinOps)

**[PP-F5-OPS-009] (MUST)** Panel de costes por servicio (cloud, Keycloak, OpenSearch, Redis, IA, mensajería, pagos, facturación electrónica, email) con etiquetas por entorno; coste por organización activa y por consulta firmada como métricas unitarias; revisión mensual; presupuestos con alertas al 80 %; política de apagado de entornos efímeros; `capacity.md` (F3) actualizado trimestralmente con proyección a 12 meses.

Estimación orientativa de producción tras la migración (mensual): Kubernetes gestionado 3 nodos (150–300 USD), PostgreSQL multi-AZ (150–400 USD), Redis (40–80), OpenSearch 2 nodos (120–250), object storage + backups cross-region (30–80), WAF/CDN (50–150), observabilidad gestionada (50–200), status page (0–100), guardias (herramienta, 20–50), IA y mensajería según uso. Total base ≈ 700–1.600 USD/mes antes de IA/SMS.

## 10. Gestión de cambios y releases

**[PP-F5-OPS-010] (MUST)** Releases semanales con *changelog* público (help center), despliegues *rolling* con *canary* (10 % de tráfico 15 min con comparación automática de tasa de error y p95; promoción o rollback automático), ventana de congelación en fechas críticas (cierre de mes para facturación), *feature flags* para funcionalidades nuevas (tabla `platform.feature_flags` + evaluación en Go + exposición al front), toda migración de BD revisada por 2 personas y ensayada en staging con datos a escala, registro de cambios de infraestructura (IaC en repo: Terraform/OpenTofu para cloud, Helm para K8s; sin cambios manuales).

## 11. Game days

**[PP-F5-OPS-011] (SHOULD)** Semestrales: pérdida de zona, caída del proveedor de pagos (verificar que no se restringe nadie), caída del proveedor fiscal (contingencia), caída de Keycloak (degradación: tokens vigentes siguen; mensaje de login), fuga simulada de credenciales (rotación completa cronometrada); informes y acciones en tracking.
