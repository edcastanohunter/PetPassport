# F3 · Modelo de Amenazas (STRIDE Threat Model) — PP-F3-SEC-009

> Estado: Aprobado · Fecha: 2026-11-05 · Propietario: Seguridad / Arquitectura  
> Requisito: `PP-F3-SEC-009` (MUST)  

---

## 1. Alcance y Nuevas Superficies de Ataque en F3

Fase 3 introduce nuevos componentes arquitectónicos que expanden la superficie de ataque:
1. **OpenSearch**: Almacén secundario desnormalizado para pacientes, registros clínicos, medicamentos y organizaciones.
2. **Redis**: Capa compartida de caché L2 y motor de Rate Limiting distribuido.
3. **asynq (Redis DB 1)**: Sistema de colas de trabajos en segundo plano (`critical`, `default`, `low`).
4. **Directorio Público**: Endpoints no autenticados (`/directory/organizations`) con listado de clínicas y ubicación geográfica.
5. **Web Push & PWA**: Notificaciones vía navegador usando el estándar Web Push (VAPID RFC 8291/8292).
6. **Escaneo Antivirus (ClamAV)**: Ingestion y validación de archivos binarios/PDFs en `media:scan`.
7. **CDN y Estáticos**: Distribución de activos estáticos e imágenes prefirmadas.
8. **Múltiples Réplicas (`api` y `worker`)**: Despliegue concurrente con leases distribuidos.

---

## 2. Análisis STRIDE

### 2.1 Spoofing (Suplantación de Identidad)

| Amenaza | Vector | Mitigación Implementada | Estado |
|---|---|---|---|
| **S1**: Suplantación de servidor Web Push (falso endpoint) | Un actor malicioso envía suscripciones con endpoints HTTP no seguros o maliciosos para capturar payloads. | Validación estricta de esquema HTTPS y verificación de endpoints pertenecientes a proveedores push reconocidos (Google FCM, Mozilla Autopush, Apple Push). | Implementado (SEC-007) |
| **S2**: Suplantación de nodo worker para robo de lease | Un worker clonado intenta ejecutar proyecciones con orden estricto simultáneamente. | `platform.worker_leases` con renovación cada 10 s y comprobación de expiración (`INSERT ON CONFLICT DO UPDATE WHERE expires_at < now()`). | Implementado (ARQ-009, QA-004) |
| **S3**: Falsificación de identidad en consultas OpenSearch | Búsqueda clínica realizada con un `org_id` ajeno en la ruta. | Middleware `authz.Can` valida membresía activa del actor en la organización antes de procesar la búsqueda. | Implementado (SEC-001) |

---

### 2.2 Tampering (Manipulación de Datos)

| Amenaza | Vector | Mitigación Implementada | Estado |
|---|---|---|---|
| **T1**: Envenenamiento de caché Redis (*Cache Poisoning*) | Un atacante altera claves o se pierde un evento de invalidación dejando datos obsoletos o corruptos. | Serialización con `msgpack` firmada con prefijos de versión; TTLs estrictos y cortos (5 s - 300 s) como red de seguridad (*safety net*); nunca se guarda PII de contacto en caché. | Implementado (ARQ-006, SEC-003) |
| **T2**: Alteración de mappings de OpenSearch | Desalineación no controlada de campos que permita indexar o leer datos no autorizados. | `search mapping-check` en CI/CD que compara los esquemas locales versionados contra el cluster vivo y bloquea el despliegue si hay drift; `dynamic: strict` en los índices. | Implementado (ARQ-003, SEC-002) |
| **T3**: Manipulación de adjuntos clínicos con Malware | Un actor sube un archivo PDF con exploits o malware incrustado para atacar veterinarios o tutores. | Escaneo obligatorio mediante ClamAV (`media:scan`); estado `pending` bloquea descargas (409); archivos infectados se aíslan en `quarantine/` (410) y generan alerta de seguridad. | Implementado (ARQ-014, SEC-005) |

---

### 2.3 Repudiation (Repudio)

| Amenaza | Vector | Mitigación Implementada | Estado |
|---|---|---|---|
| **R1**: Descarga o exportación masiva no atribuible | Un veterinario o recepcionista exporta cohortes de pacientes y niega haberlo hecho. | Auditoría inmutable obligatoria en `platform.audit_log` con `actor_id`, `reason` justificado, IP hash y límite estricto de ≤ 1.000 filas (RN-20). | Implementado (SEC-004, SEC-006) |
| **R2**: Acciones administrativas sobre índices y caché sin registro | Un administrador ejecuta `reindex` o vacía la caché de la plataforma sin dejar rastro. | Header obligatorio `X-PP-Reason`; registro en `search.reindex_jobs` y `audit_log`; bloqueo con `ADMIN_REASON_REQUIRED` (422) si falta motivo. | Implementado (API-009, API-011) |

---

### 2.4 Information Disclosure (Fuga de Información)

| Amenaza | Vector | Mitigación Implementada | Estado |
|---|---|---|---|
| **I1**: Fuga de autorización en búsqueda clínica (IDOR masivo) | Búsqueda global OpenSearch devuelve historiales de mascotas sin vínculo activo ni custodia. | Función `accessFilter` obligatoria construida en `internal/search/app`; post-filtro en memoria `PetAccess` que descarta cualquier documento fuera de ámbito; test de fuga gate con 0 discrepancias. | Implementado (ARQ-002, SEC-001) |
| **I2**: Exposición de ubicación exacta de clínicas en directorio | El directorio público revela coordenadas exactas de clínicas en zonas sensibles o domiciliarias. | Coordenadas `geo` redondeadas a 3 decimales (≈ 100 metros) en la respuesta pública; dirección exacta nunca se expone; teléfonos solo con opt-in explícito (`directory_show_phone`). | Implementado (DOM-010, SEC-008) |
| **I3**: Fuga de datos clínicos en notificaciones Web Push | Notificaciones push intermediadas por servidores de terceros exponen diagnósticos o nombres de medicamentos. | Cifrado RFC 8291 con claves VAPID; carga útil mínima y genérica (p. ej. "Luna tiene una cita mañana a las 10:00"); sin datos clínicos sensibles ni diagnósticos en el payload. | Implementado (ARQ-015, SEC-007) |
| **I4**: Exposición de puertos OpenSearch y Redis | Ataques directos a los puertos 9200 (OpenSearch) o 6379 (Redis) desde internet. | Aislamiento en red privada interna sin binding público; autenticación requerida en producción; TLS habilitado. | Implementado (SEC-002, SEC-003, INF-002) |

---

### 2.5 Denial of Service (Denegación de Servicio)

| Amenaza | Vector | Mitigación Implementada | Estado |
|---|---|---|---|
| **D1**: Scraping y agotamiento de recursos por búsquedas pesadas | Consultas complejas masivas agotan CPU de OpenSearch o conexiones a base de datos. | Rate limiting distribuido por IP, usuario y organización (Redis Lua sliding window); `429 RATE_LIMITED` con cabeceras IETF; detección de scraping en `AbuseDetector` (> 2.000 req/día o > 500 pacientes/día). | Implementado (ARQ-007, SEC-004) |
| **D2**: Caída de OpenSearch bloquea la atención médica en clínicas | Indisponibilidad del cluster OpenSearch imposibilita atender urgencias. | Circuit breaker (5 fallos en 30 s) con degradación automática a PostgreSQL (`FallbackPG` con trigramas e ILIKE) y cabecera `X-PP-Search-Mode: degraded`. | Implementado (ARQ-004, PRD-010) |
| **D3**: Sobrecarga de cola asynq por tareas duplicadas | Reintentos compulsivos saturan la cola de trabajos en segundo plano. | Deduplicación determinista por `TaskID` (`Unique`); 10 reintentos con backoff exponencial máximo de 1 h; envío a DLQ (`archived`) al agotar reintentos. | Implementado (ARQ-008) |
| **D4**: Ataque de bomba zip / descompresión en PDFs | Subida de archivos que se expanden a gigabytes en memoria. | Límite estricto de 10 MB + 1 byte en `readUpload`; procesador `document.Process` con límite de 50 páginas y rechazo de PDFs con JavaScript o streams comprimidos anómalos. | Implementado (ARQ-011/F2, SEC-005) |

---

### 2.6 Elevation of Privilege (Elevación de Privilegios)

| Amenaza | Vector | Mitigación Implementada | Estado |
|---|---|---|---|
| **E1**: Reclamación ilegítima de paciente provisional | Un usuario malicioso intenta reclamar un paciente provisional sin ser el tutor legal. | Token criptográfico aleatorio de alta entropía (32 bytes) enviado exclusivamente al contacto registrado; validación en `ClaimPet` de que el contacto verificado de la cuenta coincide con el del reclamo. | Implementado (DOM-007, SEC-007) |
| **E2**: Edición de paciente provisional por otra clínica | Una clínica competidora intenta modificar los datos de un paciente provisional antes de ser reclamado. | Invariante RN-21: solo la clínica creadora (`provisional_org_id`) tiene permisos de edición; intento externo rechazado con `PROVISIONAL_ONLY_CREATOR` (403). | Implementado (DOM-007, RN-21) |
| **E3**: Modificación no autorizada de catálogos farmacéuticos | Un usuario clínico añade medicamentos falsos o aprueba sugerencias sin rol de SuperAdmin. | Permiso `catalog:admin` requerido en todas las mutaciones del catálogo; los veterinarios solo pueden emitir sugerencias pendientes de revisión. | Implementado (DOM-005, API-003) |

---

## 3. Matriz de Riesgo Residual

| ID Riesgo | Componente | Severidad Inicial | Severidad Residual | Razón de Aceptación |
|---|---|---|---|---|
| R-F3-01 | OpenSearch Desactualizado | Alta | Baja | Post-filtro `PetAccess` descarta resultados no autorizados en tiempo real si el índice tiene lag. |
| R-F3-02 | Falla de Redis | Media | Baja | Circuit breaker con *fail-open* en rate limiting y bypass directo a DB en caché evitan cortes de servicio. |
| R-F3-03 | Falso Positivo ClamAV | Media | Baja | SuperAdmin puede revisar adjuntos en cuarentena y restaurarlos si es necesario. |
| R-F3-04 | Reindex Concurrentes | Media | Baja | Leases de worker y locks en `search.reindex_jobs` previenen ejecuciones simultáneas del mismo alias. |

---

## 4. Conclusión

El diseño de Fase 3 cumple con los principios de defensa en profundidad, privilegios mínimos y aislamiento modular de PetPassport (ADR-001, ADR-006). Todos los vectores identificados cuentan con controles técnicos automatizados verificables en las suites de integración y CI.
