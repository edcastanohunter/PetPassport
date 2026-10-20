# Roadmap de Ejecución Progresiva — PetPassport

> Estado: v1.0 · Fecha: 2026-10-20 · Propietario: Arquitectura / Producto
> Este documento es la visión general. Cada fase tiene su carpeta `docs/phase-N/` con specs completas.

---

## 1. Visión del producto

**PetPassport** es el pasaporte digital de salud de la mascota. Pertenece al **tutor** (dueño),
viaja con la mascota entre **clínicas veterinarias** y se conecta con **farmacias y tiendas aliadas**
para convertir una receta en un pedido cotizado automáticamente.

### Promesas de producto

1. **Historia clínica única e inmutable** — nunca se pierde, nunca se altera, siempre auditable.
2. **Carnet de vacunas y recordatorios** que funcionan sin que el tutor tenga que acordarse.
3. **De receta a pedido en segundos** — foto de la receta → OCR/IA → cotización en farmacias aliadas.
4. **Multi-clínica por diseño** — el dato es del tutor; las clínicas acceden mediante un vínculo de atención.

### Principios rectores (no negociables)

| Principio | Implicación técnica |
|---|---|
| El dato clínico es del tutor | Modelo *owner-centric*: la clínica accede por `CareLink`, no por propiedad. (ADR-004) |
| Inmutabilidad clínica | Event Sourcing para todo lo clínico y para todo lo que tenga historia relevante. (ADR-002) |
| Seguridad por diseño | WAF + gateway + validación estricta + autorización *deny-by-default* desde la Fase 1. (ADR-006) |
| Evolución sin reescritura | Monolito modular con límites de contexto estrictos, extraíble a servicios cuando lo justifique la carga. (ADR-001) |
| Comprar lo genérico, construir lo core | Keycloak para identidad; OCR/IA con proveedores; el core (clínico, prescripciones, pedidos) es propio. |

---

## 2. Mapa de contextos (Context Map)

Subdominios según valor para el negocio:

- **Core**: Clinical Records, Prescriptions & Pharmacy Orders.
- **Supporting**: Pet Registry, Partner Network, Scheduling, Search & Reporting.
- **Generic**: Identity & Access (Keycloak), Notifications, Billing.

```
                     ┌────────────────────────┐
                     │  Identity & Access     │  (Keycloak · generic · upstream)
                     │  UserAccount           │
                     └───────────┬────────────┘
                 Conformist+ACL  │
      ┌──────────────────────────┼─────────────────────────────┐
      ▼                          ▼                             ▼
┌───────────────┐   C/S   ┌──────────────────┐        ┌────────────────────┐
│ Partner       │◄────────│ Pet Registry     │        │ Scheduling (F2)    │
│ Network       │         │ Pet, CareLink    │◄───────│ Appointment,       │
│ Organization  │         │                  │   C/S  │ ReminderRule       │
└──────┬────────┘         └────────┬─────────┘        └─────────┬──────────┘
       │                           │ C/S                        │
       │                           ▼                            │ eventos
       │                 ┌──────────────────────┐               ▼
       │                 │ Clinical Records (F2)│      ┌────────────────────┐
       │                 │ MedicalRecord,       │─────►│ Notifications (F2) │
       │                 │ Vaccination, Visit   │      └────────────────────┘
       │                 └──────────┬───────────┘
       │                            │ Published Language (eventos)
       ▼                            ▼
┌───────────────────┐    ┌─────────────────────────┐    ┌──────────────────────┐
│ Prescriptions &   │◄───│ Search & Reporting (F3) │    │ Billing (F5)         │
│ Pharmacy Orders   │    │ OpenSearch read models  │    │ Plans, Subscriptions │
│ (F4) · OCR/ACL    │    └─────────────────────────┘    └──────────────────────┘
│ partner APIs      │
└───────────────────┘
```

Leyenda: **C/S** = Customer/Supplier (el downstream consume por puerto/consulta); **ACL** = Anti-Corruption Layer;
**Published Language** = eventos de integración versionados.

Relaciones clave:

- *Identity* es upstream de todos: emite tokens; los contextos **no** conocen Keycloak, solo un `Principal`.
- *Pet Registry* es la columna vertebral: toda historia clínica cuelga de un `PetID`.
- *Partner Network* define quién es una organización válida; *Pet Registry* consulta por puerto.
- *Clinical Records* publica eventos que alimentan Notifications, Search y, en F4, Prescriptions.

---

## 3. Fases

Se mantienen las cuatro fases solicitadas y se añaden dos más (F5, F6) necesarias para operar un SaaS público
con ingresos. Además hay **pistas transversales** presentes en todas las fases: Seguridad, Observabilidad,
Calidad y Experiencia de desarrollo.

| Fase | Nombre | Objetivo de negocio | Contextos | Entregable verificable |
|---|---|---|---|---|
| **F1** | Fundación y MVP Core (Onboarding & Identity) | Usuarios reales pueden registrarse, crear mascotas, registrar su clínica y vincularse. | Identity & Access, Pet Registry, Partner Network | MVP público en staging con WAF, Keycloak, Event Store v0 y UI tutor/clínica/admin. |
| **F2** | Clínica y CQRS-ES (Medical Records) | La clínica opera consultas, vacunas y citas; el historial es inmutable y reconstruible. | Clinical Records, Scheduling, Notifications v0 | Consulta completa + carnet + cita con recordatorio; proyecciones asíncronas con *replay*. |
| **F3** | Search & Performance | Búsqueda instantánea en historiales y catálogo; latencias p95 bajo objetivo. | Search & Reporting | OpenSearch + Redis (caché, rate limit, colas); pruebas de carga con SLOs cumplidos. |
| **F4** | Integración B2B y Automatización (Pharmacy & OCR) | Receta → cotización automática en farmacias aliadas. | Prescriptions & Pharmacy Orders, Partner Integration | Pipeline OCR/IA con confirmación humana, API partner con gateway/WAF dedicado, portal farmacia. |
| **F5** | Operación SaaS y Monetización | Planes, facturación, soporte, continuidad del negocio. | Billing, Ops | Suscripciones (Stripe o similar), límites por plan, DR probado, cumplimiento de datos. |
| **F6** | Escala y Evolución | Crecer sin reescribir: extracción de servicios, móvil, telemedicina. | — | Servicios extraídos donde la carga lo justifica; app móvil; analítica. |

### 3.1 Detalle por fase (resumen; la spec completa se entrega al cerrar la anterior)

#### F1 — Fundación y MVP Core → `docs/phase-1/`
- Keycloak como código; roles; onboarding; perfiles de tutor y mascota; organizaciones (clínica/farmacia) y membresías; vínculo mascota–clínica; panel SuperAdmin.
- **Event Store v0** en PostgreSQL con proyecciones síncronas y *outbox* escrito (ver ADR-002: se adelanta el núcleo del event store a F1 para evitar migrar de CRUD a ES en F2).
- Borde: WAF gestionado + Traefik; CI/CD; observabilidad base.

#### F2 — Clínica y CQRS-ES → `docs/phase-2/`
- *Clinical Records*: `Encounter` (consulta SOAP: borrador → firmada → adendas) y `VaccinationCard` (vacunas y preventivos, protocolos por especie/país, entradas declaradas) como agregados; el historial clínico es un read model (`medical_timeline`); PDF firmado con hash verificable; carnet PDF/QR y enlace compartido (ADR-010).
- *Scheduling*: `Appointment` y `ProfessionalAgenda` como agregados; no-solapamiento por restricción de exclusión transaccional (ADR-011).
- *Notifications v0*: process manager sobre el outbox, recordatorios programados, plantillas es/en, preferencias, dead-letter (ADR-012).
- **Event Store v1**: `worker`, suscripciones catch-up con checkpoints, orden total por advisory lock, snapshots, upcasters, rebuild, fail-stop (ADR-009).
- Solicitud de vínculo iniciada por la clínica con aprobación del tutor; perfil de mascota con alertas.

#### F3 — Search & Performance → `docs/phase-3/`
- OpenSearch como read model de búsqueda (índices `pets`, `clinical`, `medications`, `organizations`) alimentado por suscripción catch-up, autorización denormalizada en documentos, reindex sin downtime, degradación a PostgreSQL (ADR-013).
- Catálogo de medicamentos con identificadores estables y `MedicationItem` v2 (ADR-016); cohortes y campañas; directorio público opt-in; panel de plataforma.
- Redis para caché compartida, rate limiting distribuido y asynq (trabajos no ordenados); PostgreSQL sigue siendo la fuente durable (ADR-014).
- Escalado horizontal: `api`×N, `worker`×N con leases; Helm chart; Kubernetes en F5 (ADR-015). SLOs, k6 con datos sintéticos a escala, CDN, game day.
- Pendientes de F2: ClamAV, Web Push, paciente provisional, accesos anómalos.

#### F4 — Integración B2B y Automatización → `docs/phase-4/`
- *Prescriptions*: `Prescription` como agregado con dos orígenes — emitida por veterinario con credencial verificada (firma, PDF, QR, verificación pública) o externa subida por el tutor; validez, refills, dispensaciones; controladas no pedibles (ADR-018).
- Pipeline OCR/IA en el worker: preprocesado → modelo de visión con salida estructurada (Claude, proveedor intercambiable) → emparejamiento con catálogo → **confirmación humana obligatoria**; evaluación continua con dataset y presupuesto (ADR-019).
- *Pharmacy Orders*: `QuoteRequest` como saga y `Order` como agregado; pago en la farmacia; sustituciones validadas; minimización de datos por etapa (ADR-020).
- *Partner Integration*: tres niveles (portal sin integración, `partner/v1` con OAuth2 + webhooks firmados + sandbox + simulador, adaptador saliente con ACL); borde `partners.<dominio>` con WAF propio, egress proxy anti-SSRF; producto de gateway diferido con criterio (ADR-017, ADR-021).
- Portal de farmacia completo; administración de partners, reglas regulatorias por país, presupuesto de IA.

#### F5 — Operación SaaS y Monetización → `docs/phase-5/`
- *Billing*: `BillingAccount` y `Subscription` propios (planes versionados, prueba 30 días, prorrateo, dunning con gracia); proveedor de pagos y proveedor de facturación fiscal (DIAN) abstraídos y elegidos por jurisdicción (ADR-022); entitlements consultados por puerto en el command bus con límites blandos y medición de uso por eventos (ADR-025).
- *Privacy*: crypto-shredding por sujeto con KMS, migración única de eventos históricos, derechos del titular automatizados, retención automática (ADR-023).
- *Operación*: Kubernetes multi-zona, RPO 5 min / RTO 1 h, backups cross-region, simulacros, on-call, incidentes y postmortems, status page, SLA con créditos, FinOps (ADR-024).
- Soporte con impersonación consentida y auditada; SMS/WhatsApp; cumplimiento (registro de tratamientos, DPIA, DPAs, revisión de accesos, auditoría exportable, fiscal).

#### F6 — Escala y Evolución
- Extracción a servicios (Search, OCR, Notifications) solo si la carga lo justifica; Kubernetes.
- App móvil (PWA → nativa), telemedicina, analítica/BI, marketplace de servicios.

### 3.2 Dependencias entre fases

```
F1 ──► F2 ──► F3 ──► F4 ──► F5 ──► F6
       │             ▲
       └─────────────┘   (F4 requiere Redis/colas de F3 para la saga de cotización)
```

F3 puede solaparse con el final de F2; F5 puede iniciar en paralelo a F4 (billing no depende de OCR).

### 3.3 Estimación orientativa

Para un equipo de 3–5 personas (1 backend senior, 1 backend, 1 frontend, 1 full-stack/DevOps, PM/QA parcial):

| Fase | Duración | Nota |
|---|---|---|
| F1 | 6–8 semanas | Incluye setup de infraestructura y Keycloak. |
| F2 | 8–10 semanas | El core clínico es el más denso en reglas. |
| F3 | 4–6 semanas | |
| F4 | 10–12 semanas | Depende de la disponibilidad de APIs de partners. |
| F5 | 6–8 semanas | |

Las estimaciones se recalibran al cerrar cada fase con la velocidad real medida en `docs/tracking/`.

---

## 4. Arquitectura evolutiva

```
F1  [WAF]→[Traefik]→[api (Go monolito modular)]→[PostgreSQL: es.* + read_*]   [Keycloak]  [MinIO/S3]
F2  + [worker (mismo binario, modo worker)] consume es.outbox → proyecciones async, emails
F3  + [OpenSearch] + [Redis] (caché, rate limit, asynq)
F4  + [gateway partner (Kong/APISIX) + WAF dedicado] + [ocr-worker] + ACL por partner
F5  + [billing] + DR multi-zona
F6  + extracción selectiva a servicios + Kubernetes
```

El binario Go es único (`cmd/api`, `cmd/worker`, `cmd/migrate`) hasta F6. Los módulos (bounded contexts)
viven en `internal/<contexto>/` con reglas de importación verificadas en CI.

---

## 5. Seguimiento del avance

- Fuente de verdad: `docs/tracking/phase-N.md` (una fila por requisito, estado controlado).
- Verificación automática: `scripts/specs-status.sh --check` en CI; `scripts/specs-status.sh` para el resumen.
- Hitos de GitHub = hitos de la fase (`M1.0`…`M1.5`). Issues y PRs referencian IDs de requisitos.
- Detalle en [`tracking/README.md`](tracking/README.md).

---

## 6. Riesgos principales y mitigaciones

| Riesgo | Impacto | Mitigación |
|---|---|---|
| Migrar de CRUD a Event Sourcing a mitad de camino | Alto | Event Store v0 desde F1 (ADR-002). |
| Sobre-ingeniería de ES en agregados sin historia relevante | Medio | Identity es CRUD; ES solo donde hay auditoría/historia (ADR-002). |
| Tenancy mal modelada (clínica "dueña" del paciente) | Alto | Modelo owner-centric con `CareLink` (ADR-004). |
| Datos personales en eventos inmutables vs derecho de supresión | Alto | Diseño de *crypto-shredding* desde F1; PII en VO cifrables (SEC-009). |
| Keycloak como cuello de botella de autorización | Medio | Keycloak solo autentica; la autorización vive en la app (ADR-003). |
| Dependencia de APIs de partners inexistentes/inestables (F4) | Alto | ACL por partner, sandbox propio, contrato mínimo publicado en F3. |
| OCR con baja precisión | Medio | Confirmación humana obligatoria; proveedor intercambiable; métricas de precisión. |
| Equipo pequeño y alcance amplio | Alto | MoSCoW estricto; `MUST` define el corte de cada fase. |

---

## 7. Supuestos

- Mercado inicial hispanohablante (es-CO como locale por defecto, en disponible). Normativa de datos: ley local de protección de datos personales + diseño compatible con GDPR.
- Las farmacias aliadas expondrán API REST o aceptarán integrar contra un contrato publicado por PetPassport.
- Infraestructura inicial en un proveedor cloud con PostgreSQL gestionado; Kubernetes diferido (ADR-008).
