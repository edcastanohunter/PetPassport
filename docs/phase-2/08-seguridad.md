# F2 · 08 — Seguridad, autorización y privacidad clínica

Controles de F1 (08-seguridad) vigentes. F2 introduce datos de salud animal, documentos firmados y páginas públicas.

## 1. Threat model F2

**[PP-F2-SEC-007] (MUST)** Actualización del modelo STRIDE con los componentes nuevos (worker, outbox, PDF, enlaces públicos, webhooks de email). Amenazas prioritarias:

| Amenaza | Control |
|---|---|
| Clínica accede a historial de mascotas no vinculadas (IDOR por `pet_id`) | `PetAccess` como única puerta; suite de authz ampliada (SEC-001). |
| Clínica ve consultas de otras clínicas tras revocación | Custodia solo sobre encuentros propios (`encounter_custody`). |
| Alteración de consultas firmadas | Append-only + hash verificable + PDF con hash. |
| Enumeración de mascotas/tutores vía `pet-lookup` | Resultados mínimos, rate limit 20/h, tiempo constante, auditoría. |
| Fuga por enlaces compartidos | Token 256 bits, caducidad, PIN, revocación, sin PII, `noindex`, rate limit por token. |
| Adjuntos maliciosos (PDF con JS, imágenes polyglot) | Validación estructural, re-encode, prefirmadas cortas, sin ejecución; antivirus en F3. |
| Worker con `Principal = system` ejecutando comandos | Lista cerrada de comandos programables; `system` no pasa por authz pero cada PM declara qué comandos puede emitir (test). |
| Webhook de email falsificado | Firma HMAC del proveedor verificada; idempotencia por `message_id`. |
| Emails con datos clínicos | Plantillas sin contenido clínico (NOT-003). |

## 2. Autorización (ampliación de la tabla IAM-010)

**[PP-F2-SEC-001] (MUST)** Abreviaturas: G tutor; L miembro de org con vínculo activo (org `active`); C miembro de org con custodia sobre el encuentro; V = rol `veterinarian`; RA = `clinic_admin`; RC = `receptionist`; A `super_admin`.

| Acción | G | RA | V | RC | A |
|---|---|---|---|---|---|
| `record:read` (timeline, resumen, peso, carnet) | ✔ | L | L | L | ✔ (auditado) |
| `encounter:list` | — | L+C | L+C | L+C | ✔ |
| `encounter:read` | ✔ si `signed` | L+C | L+C | L+C | ✔ |
| `encounter:open` | — | L | L | L | — |
| `encounter:edit` (S/O/constantes/adjuntos/motivo) | — | L (draft) | L (draft) | L (draft) | — |
| `encounter:clinical_edit` (A/P/control/visibilidad) | — | si V | L (draft) | — | — |
| `encounter:sign`, adendas | — | si V y responsable | L y responsable | — | — |
| `encounter:discard` | — | L | L (responsable) | — | ✔ |
| `card:record`, `next-due`, `card:verify` | — | L | L | L | — |
| `card:declare` | ✔ | — | — | — | — |
| `card:void` | ✔ (solo `declared` propias) | L (propias ≤ 30 d) | L (propias ≤ 30 d) | — | ✔ |
| `pet:alerts` | ✔ | L | L | L | ✔ |
| `share:manage` | ✔ | — | — | — | ✔ (revocar) |
| `appointment:request`, `accept-proposal`, `appointment:cancel` (propias) | ✔ | — | — | — | — |
| `appointment:read` | ✔ (propias) | L | L | L | ✔ |
| `appointment:manage` | — | ✔ (org) | ✔ (org) | ✔ (org) | — |
| `agenda:manage` | — | ✔ | propia (COULD) | — | — |
| `carelink:request`, `pet-lookup` | — | ✔ | ✔ | ✔ | — |
| `org:stats:read` | — | ✔ | ✔ | — | ✔ |
| `catalog:admin` | — | — | — | — | ✔ |

Reglas:
- `PetAccess.CanOrgAccess` devuelve `linked` (vínculo activo y org activa), `custody` (solo encuentros propios) o `none`. Toda consulta clínica por org filtra por ese nivel.
- Las acciones clínicas comprueban el **rol dentro de la organización del encuentro**, no cualquier membresía.
- `super_admin` lee datos clínicos solo con motivo (`X-PP-Reason` obligatorio en `/admin` y rutas de lectura con `?admin_reason=`); se audita.
- La suite de autorización (F1 SEC-005) se amplía a todas las acciones nuevas × roles × niveles de acceso (`linked`, `custody`, `none`, org suspendida).

## 3. Enlaces compartidos y páginas públicas

**[PP-F2-SEC-002] (SHOULD)**

- Token: 32 bytes CSPRNG, base64url, solo se muestra al crearlo; en BD `SHA-256`. PIN: Argon2id; 5 intentos → bloqueo del enlace 1 h.
- Caducidad obligatoria (≤ 90 d); revocación inmediata; ≤ 10 activos por mascota.
- Respuesta sin datos de contacto del tutor, microchip enmascarado (últimos 4), fotos por URL prefirmada de 5 min.
- WAF: `/share/*` y `/verify/*` con rate limit 30/min/IP y por token; `X-Robots-Tag: noindex, nofollow`; `Referrer-Policy: no-referrer`.
- Cada acceso: `access_count++`, `audit_log{action: share.access, ip_hash}`; el tutor ve el contador.

## 4. Adjuntos

**[PP-F2-SEC-003] (MUST)**

- Tipos: JPEG/PNG/WebP (re-encode, EXIF sin GPS) y PDF (validación `pdfcpu`, rechazo de JavaScript/formularios/adjuntos embebidos, ≤ 10 MB, ≤ 50 páginas).
- Descarga por URL prefirmada 15 min con `Content-Disposition: attachment` para PDF; `Content-Type` fijado por el servidor.
- Acceso de tutor solo a adjuntos `visible_to_guardian`; cambiar visibilidad exige motivo y queda auditado.
- Antivirus (ClamAV) diferido a F3 (COULD aquí): los PDFs se abren en el navegador del usuario en *sandbox* (`sandbox` en iframe) hasta entonces.

## 5. Integridad de documentos firmados

**[PP-F2-SEC-004] (MUST)**

- `content_hash` según ARQ-010 (JSON canónico RFC 8785 de los eventos hasta la versión firmada); se guarda en el evento `EncounterSigned` **y** en el read model; `GET /verify/encounters/{id}?hash=` recomputa desde `es.events`.
- Prueba: cualquier alteración simulada del read model se detecta; el PDF incluye el hash y la URL de verificación (QR).
- Firma criptográfica con clave del servidor (JWS sobre el hash) diferida a F5 si se requiere valor probatorio reforzado; el diseño deja `signature.jws` opcional en el DTO.

## 6. Auditoría de accesos clínicos

**[PP-F2-SEC-005] (MUST)**

- Toda lectura de `record:read`, `encounter:read`, adjuntos y carnet por un **miembro de organización** genera `audit_log{action: clinical.read, actor, org, pet, resource}` (una entrada por request, no por fila). Las lecturas del tutor sobre sus mascotas no se auditan (son suyas).
- El tutor puede ver "Quién ha accedido" (`GET /pets/{id}/access-log`, COULD en F2, SHOULD en F5) con organización y fecha (sin nombre de la persona).
- `audit_log` particionado mensual (DAT-008). Alerta de patrón anómalo (> 200 fichas distintas/día por un usuario) → revisión manual (F3 automatiza).

## 7. Privacidad

**[PP-F2-SEC-006] (MUST)**

- Clasificación: contenido clínico = dato de salud del **animal** (no dato personal), pero está vinculado a un tutor identificable → se trata como dato personal a efectos de acceso, retención y exportación.
- Minimización: emails sin contenido clínico; enlaces públicos sin contacto; `pet-lookup` sin contacto.
- Retención: consultas y carnet, indefinida (historia del animal); al anonimizar un tutor (F5 crypto-shredding), los encuentros permanecen vinculados a un `PetID` cuyo tutor es irrecuperable.
- Exportación (manual F1 → semiautomática F2): `cmd/worker export --user <id>` genera JSON con cuentas, mascotas, timeline, carnet, citas y entregas de notificación; runbook de entrega segura.
- Consentimiento: el aviso de privacidad se actualiza (nueva versión) describiendo compartición clínica, enlaces públicos y recordatorios; el onboarding exige re-aceptación (PRD-003 de F1).
- Información a clínicas: los términos de organización incluyen deber de custodia y uso limitado de datos de contacto.
