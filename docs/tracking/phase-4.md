# Tracking — Fase 4: Integración B2B y Automatización (Pharmacy & OCR)

> Fuente de verdad del estado. Editar el estado **solo aquí**. Validar con `scripts/specs-status.sh --check`.
> Estados: `PENDIENTE` `EN_DISENO` `EN_PROGRESO` `IMPLEMENTADO` `VERIFICADO` `BLOQUEADO` `DESCARTADO`.

| ID | Área | Requisito | Prio | Hito | Estado | Issue | PR | Notas |
|---|---|---|---|---|---|---|---|---|
| PP-F4-PRD-001 | PRD | Credencial profesional del veterinario con verificación | MUST | M4.0 | IMPLEMENTADO | | | Credenciales profesionales veterinarias con moderación admin |
| PP-F4-PRD-002 | PRD | Emitir receta firmada desde consulta o directa; PDF/QR | MUST | M4.0 | IMPLEMENTADO | | | Emisión de receta firmada desde consulta o directa con PDF y QR de verificación |
| PP-F4-PRD-003 | PRD | Verificación pública de receta | MUST | M4.0 | IMPLEMENTADO | | | Verificación pública contra hash canónico RFC 8785 de eventos |
| PP-F4-PRD-004 | PRD | Revocar receta | MUST | M4.0 | IMPLEMENTADO | | | Revocación de receta con motivo y bloqueo si hay dispensaciones previas |
| PP-F4-PRD-005 | PRD | Subir receta externa y ver extracción | MUST | M4.1 | IMPLEMENTADO | | | Subida multipágina (1–5) con validación de formato/tamaño y extracción OCR con Claude/Tesseract |
| PP-F4-PRD-006 | PRD | Revisar y confirmar extracción lado a lado | MUST | M4.1 | IMPLEMENTADO | | | Revisión lado a lado con visor zoom/pan, confianza de ítems, edición y gate obligatorio de atestación |
| PP-F4-PRD-007 | PRD | Mis recetas (tutor) | MUST | M4.1 | IMPLEMENTADO | | | Bandeja de recetas tutor con filtros por mascota/estado/origen, badges de verificación y acciones |
| PP-F4-PRD-008 | PRD | Solicitar cotización a farmacias | MUST | M4.2 | IMPLEMENTADO | | | Orquestación de cotizaciones multi-farmacia con SLA, bloqueo de controlados y consentimiento |
| PP-F4-PRD-009 | PRD | Tablero de cotizaciones en tiempo real y comparación | MUST | M4.2 | IMPLEMENTADO | | | Tablero en tiempo real con SSE, cuenta atrás, comparación de precios y selección de oferta |
| PP-F4-PRD-010 | PRD | Pedido, seguimiento y dispensación reflejada | MUST | M4.2 | IMPLEMENTADO | | | Seguimiento de ciclo completo de pedido tutor y dispensación con batch y fecha de expiración |
| PP-F4-PRD-011 | PRD | Sustituciones: aceptar/rechazar | MUST | M4.2 | IMPLEMENTADO | | | Gestión de propuestas de sustitución farmacéutica con aprobación explícita del tutor |
| PP-F4-PRD-012 | PRD | Portal: bandeja y formulario de cotización; validación de recetas externas | MUST | M4.2 | IMPLEMENTADO | | | Bandeja farmacéutica con pestañas por estado, visor de receta vinculada y formulario de cotización |
| PP-F4-PRD-013 | PRD | Portal: pedidos y registro de dispensación | MUST | M4.2 | IMPLEMENTADO | | | Flujo operativo de farmacia: confirmación, preparación, despacho y entrega con lote/vencimiento |
| PP-F4-PRD-014 | PRD | Portal: catálogo/mapeo, zonas, modalidades, horarios | MUST | M4.2 | IMPLEMENTADO | | | Configuración de farmacia: tipo de integración, zonas de cobertura y mapeo de catálogo |
| PP-F4-PRD-015 | PRD | Portal: credenciales API, webhooks, sandbox | MUST | M4.3 | IMPLEMENTADO | | | Pestañas de credenciales OAuth2, rotación de secretos con copia única, entorno sandbox y webhooks en PharmacySettingsView |
| PP-F4-PRD-016 | PRD | Portal: métricas y SLA | SHOULD | M4.5 | PENDIENTE | | | |
| PP-F4-PRD-017 | PRD | Veterinario: estado de recetas y avisos de sustitución | SHOULD | M4.2 | IMPLEMENTADO | | | Notificación de sustituciones aceptadas y actualización de estado de receta para veterinario |
| PP-F4-PRD-018 | PRD | SuperAdmin: partners, reglas regulatorias, presupuesto IA, monitor | MUST | M4.3 | IMPLEMENTADO | | | Vistas AdminPartnersPage, AdminRegulatoryPage, AdminAIPage y AdminOCRPage con acciones auditadas vía ReasonDialog |
| PP-F4-PRD-019 | PRD | Notificaciones del ciclo receta→pedido | MUST | M4.2 | IMPLEMENTADO | | | Eventos de notificación síncronos y asíncronos para el ciclo receta→cotización→pedido |
| PP-F4-PRD-020 | PRD | Sustancias controladas: emitir sí, pedir no | MUST | M4.0 | IMPLEMENTADO | | | Detección de sustancias de control especial (flag controlado en catálogo/receta) |
| PP-F4-PRD-021 | PRD | Farmacias cercanas al solicitar cotización | SHOULD | M4.2 | IMPLEMENTADO | | | Selección y ordenación de farmacias por proximidad geográfica y cobertura de entrega |
| PP-F4-PRD-022 | PRD | Valoración de farmacia | COULD | M4.5 | PENDIENTE | | | |
| PP-F4-DOM-001 | DOM | Glosario F4 | MUST | M4.0 | IMPLEMENTADO | | | Glosario F4 documentado en especificaciones y dominio |
| PP-F4-DOM-002 | DOM | Context map F4 | MUST | M4.0 | IMPLEMENTADO | | | Context map F4 con separación Identity/Prescriptions/PharmacyOrders |
| PP-F4-DOM-003 | DOM | ProfessionalCredential en Identity | MUST | M4.0 | IMPLEMENTADO | | | ProfessionalCredential agregado en módulo Identity con ciclo de vida |
| PP-F4-DOM-004 | DOM | Agregado Prescription: comandos e invariantes R1–R9 | MUST | M4.0 | IMPLEMENTADO | | | Agregado Prescription con invariantes canónicas R1–R9 testeadas |
| PP-F4-DOM-005 | DOM | Value Objects de receta | MUST | M4.0 | IMPLEMENTADO | | | Value Objects: DoseVO, QuantityVO, CredentialRef, SignatureBlock |
| PP-F4-DOM-006 | DOM | Eventos Prescription v1 | MUST | M4.0 | IMPLEMENTADO | | | Eventos Prescription v1 con golden tests y migración de eventos |
| PP-F4-DOM-007 | DOM | Entidad Extraction y Confirmation.Diff | MUST | M4.1 | IMPLEMENTADO | | | Entidad Extraction, estados, cálculo de diferencias Confirmation.Diff y confirmación externa |
| PP-F4-DOM-008 | DOM | Agregado QuoteRequest (saga): comandos e invariantes Q1–Q8 | MUST | M4.2 | IMPLEMENTADO | | | Agregado QuoteRequest con invariantes canónicas Q1–Q8 y soporte de concurrencia |
| PP-F4-DOM-009 | DOM | Agregado Order: transiciones e invariantes O1–O8 | MUST | M4.2 | IMPLEMENTADO | | | Agregado Order con ciclo de vida completo e invariantes canónicas O1–O8 |
| PP-F4-DOM-010 | DOM | Eventos Pharmacy Orders v1 | MUST | M4.2 | IMPLEMENTADO | | | 18 eventos de dominio v1 registrados con esquemas canónicos y golden tests |
| PP-F4-DOM-011 | DOM | PartnerIntegration, ProductMapping, WebhookDelivery, PartnerCall | MUST | M4.2 | IMPLEMENTADO | | | Modelos de integración de partners B2B, mapeo de catálogo y tracking de llamadas |
| PP-F4-DOM-012 | DOM | Reglas regulatorias por país | MUST | M4.0 | IMPLEMENTADO | | | Reglas regulatorias por país cargadas desde ref.regulatory_rules |
| PP-F4-DOM-013 | DOM | Puertos F4 | MUST | M4.0 | IMPLEMENTADO | | | Puertos hexagonales y adaptadores en infra/app de prescriptions |
| PP-F4-DOM-014 | DOM | Process managers y políticas F4 | MUST | M4.2 | IMPLEMENTADO | | | Process manager de cotización con auto-cierre por timeout y proyección a pedidos |
| PP-F4-DOM-015 | DOM | SLA por partner (proyección) | SHOULD | M4.5 | PENDIENTE | | | |
| PP-F4-ARQ-001 | ARQ | Módulos nuevos y reglas de dependencia | MUST | M4.0 | IMPLEMENTADO | | | Estructura hexagonal con depguard en .golangci.yml |
| PP-F4-ARQ-002 | ARQ | Saga de cotización: orquestación, timeouts, idempotencia | MUST | M4.2 | IMPLEMENTADO | | | Saga de cotización con persistencia de eventos, timeouts y selección atómica idempotente |
| PP-F4-ARQ-003 | ARQ | PartnerGateway y niveles portal/api/adapter | MUST | M4.2 | IMPLEMENTADO | | | PartnerGateway soportando integración portal web y adaptadores B2B |
| PP-F4-ARQ-004 | ARQ | Webhooks salientes firmados con reintentos | MUST | M4.3 | IMPLEMENTADO | | | Despachador de webhooks con firma HMAC-SHA256, cabeceras X-PP-Timestamp/Signature, rotación zero-downtime y backoff exponencial |
| PP-F4-ARQ-005 | ARQ | Partner API entrante: auth, idempotencia, límites, alcance | MUST | M4.3 | IMPLEMENTADO | | | PartnerGuard con audiencia petpassport-partner-api, idempotencia en POSTs, rate limiting 600/120 rpm y endpoints /partner/v1 |
| PP-F4-ARQ-006 | ARQ | Egress: anti-SSRF, circuit breaker, proxy | MUST | M4.3 | IMPLEMENTADO | | | EgressClient hermético con validación IP/DNS anti-SSRF, bloqueo de loopback/privadas, MaxRedirects=0 y circuit breaker por host |
| PP-F4-ARQ-007 | ARQ | Simulador de partner | MUST | M4.3 | IMPLEMENTADO | | | CLI partner-sim con comandos serve (receptor de webhooks con verificación HMAC) y checklist (10 escenarios go-live) |
| PP-F4-ARQ-008 | ARQ | Entorno sandbox | MUST | M4.3 | IMPLEMENTADO | | | Realm petpassport-sandbox en Keycloak con clientes partner-api, sim y demo para pruebas herméticas |
| PP-F4-ARQ-009 | ARQ | Sitio de desarrolladores | SHOULD | M4.3 | IMPLEMENTADO | | | developers/index.html con explorador OpenAPI Scalar, guía OAuth2, webhooks, vectores HMAC, changelog y checklist |
| PP-F4-ARQ-010 | ARQ | Borde partners.<dominio> en Traefik y WAF | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-ARQ-011 | ARQ | PDF y verificación de recetas | MUST | M4.0 | IMPLEMENTADO | | | Generación de PDF con Typst, QR de verificación y subida S3/MinIO |
| PP-F4-ARQ-012 | ARQ | Pipeline OCR en worker (integración) | MUST | M4.1 | IMPLEMENTADO | | | Pipeline Asynq TypeOCRPipeline cableado con preprocesado, Claude/mock, fallback y matcher |
| PP-F4-ARQ-013 | ARQ | Contratos de minimización de datos hacia partners | MUST | M4.2 | IMPLEMENTADO | | | Payload builders de minimización estricta sin PII hasta la selección formal de cotización |
| PP-F4-ARQ-014 | ARQ | Read models F4 | MUST | M4.2 | IMPLEMENTADO | | | Read models en schema read_pharmacyorders con proyecciones transaccionales en tiempo real |
| PP-F4-API-001 | API | Endpoints de credenciales | MUST | M4.0 | IMPLEMENTADO | | | Endpoints de credenciales en /app/profile/credentials y /admin/credentials |
| PP-F4-API-002 | API | Endpoints de recetas emitidas | MUST | M4.0 | IMPLEMENTADO | | | Endpoints de prescripción: borrador, emisión, revocación y PDF |
| PP-F4-API-003 | API | Endpoints de recetas externas y extracción | MUST | M4.1 | IMPLEMENTADO | | | Endpoints declarados en OpenAPI 3.1 e implementados con authz x-pp-action y Problem Details |
| PP-F4-API-004 | API | Mis recetas y verificación pública | MUST | M4.0 | IMPLEMENTADO | | | Endpoints de mis recetas tutor, archivo y verificación pública |
| PP-F4-API-005 | API | Endpoints de cotización (tutor) y SSE | MUST | M4.2 | IMPLEMENTADO | | | Endpoints /quotes con streaming SSE y acciones tutor bajo authz quote:request y quote:read |
| PP-F4-API-006 | API | Endpoints de pedidos (tutor) | MUST | M4.2 | IMPLEMENTADO | | | Endpoints /orders con acciones de tutor y tracking de pedidos |
| PP-F4-API-007 | API | Endpoints del portal de farmacia | MUST | M4.2 | IMPLEMENTADO | | | Endpoints /pharmacies/{orgId}/* con autorización estricta para roles pharmacy_admin y pharmacist |
| PP-F4-API-008 | API | Configuración de farmacia: integración, mapeo, credenciales, webhook | MUST | M4.3 | IMPLEMENTADO | | | Endpoints OpenAPI 3.1 en /pharmacies/{orgId}/api-credentials, /webhook, /stats e importación masiva |
| PP-F4-API-009 | API | Administración: partners, reglas, presupuesto IA, monitor OCR | MUST | M4.3 | IMPLEMENTADO | | | Endpoints OpenAPI 3.1 en /admin/partners, /admin/regulatory-rules, /admin/ai y /admin/ocr |
| PP-F4-API-010 | API | Catálogo de errores F4 | MUST | M4.0 | IMPLEMENTADO | | | Catálogo de errores F4 documentado en OpenAPI y manejado en handlers |
| PP-F4-PTN-001 | PTN | Contrato partner/v1 y versionado | MUST | M4.3 | IMPLEMENTADO | | | Especificación partner-v1.yaml con esquemas canónicos, semver 1.0.0 y minimización estricta de datos |
| PP-F4-PTN-002 | PTN | Autenticación client credentials y sandbox | MUST | M4.3 | IMPLEMENTADO | | | Flujo OAuth2 client_credentials con audiencia petpassport-partner-api, rechazo cruzado de tokens y realm sandbox |
| PP-F4-PTN-003 | PTN | Endpoints partner | MUST | M4.3 | IMPLEMENTADO | | | Handlers B2B para catálogo, mapeo de SKUs individual/bulk, bandeja de cotizaciones, ofertas, sustitución y estados de orden |
| PP-F4-PTN-004 | PTN | Webhooks: eventos, firma, idempotencia | MUST | M4.3 | IMPLEMENTADO | | | Eventos quote_request.created y order.placed con firma HMAC-SHA256, soporte de doble secreto y ventana 300s |
| PP-F4-PTN-005 | PTN | Sandbox y simulador para partners | MUST | M4.3 | IMPLEMENTADO | | | Generación de credenciales sandbox en portal y binario partner-sim para simular recepción y validación |
| PP-F4-PTN-006 | PTN | Límites por partner | MUST | M4.3 | IMPLEMENTADO | | | Middleware PartnerMiddleware con límites diferenciados (600/120 rpm), Retry-After y respuesta RFC 7807 429 |
| PP-F4-PTN-007 | PTN | Onboarding y checklist go-live | MUST | M4.3 | IMPLEMENTADO | | | Documentación de 7 pasos de onboarding y suite partner-sim checklist (10/10 PASS) requerida para producción |
| PP-F4-PTN-008 | PTN | SLA y métricas para partners | SHOULD | M4.5 | PENDIENTE | | | |
| PP-F4-DAT-001 | DAT | DDL read_prescriptions | MUST | M4.0 | IMPLEMENTADO | | | DDL read_prescriptions: prescriptions, items, verifications, dispensations |
| PP-F4-DAT-002 | DAT | DDL read_pharmacyorders | MUST | M4.2 | IMPLEMENTADO | | | DDL y migración 20261108000025_pharmacy_orders.sql para schema read_pharmacyorders |
| PP-F4-DAT-003 | DAT | DDL credenciales, partner_integrations, mapeos, webhooks, partner_calls | MUST | M4.0 | IMPLEMENTADO | | | DDL identity.professional_credentials y tablas B2B base |
| PP-F4-DAT-004 | DAT | DDL ocr: uso, presupuesto, métricas, evaluación | MUST | M4.1 | IMPLEMENTADO | | | Migración SQL 20261107000024_ocr_tables.sql con schemas y tablas de presupuesto, uso y métricas |
| PP-F4-DAT-005 | DAT | ref.regulatory_rules con semilla CO | MUST | M4.0 | IMPLEMENTADO | | | Tabla ref.regulatory_rules con datos iniciales para Colombia (CO) |
| PP-F4-DAT-006 | DAT | Retención de imágenes, payloads y PII de pedidos | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-FE-001 | FE | Veterinario: credencial, emitir receta, mis recetas | MUST | M4.0 | IMPLEMENTADO | | | Vistas Vue: CredentialsPage, AdminCredentialsPage, IssuePrescriptionModal, OrgPrescriptionsPage |
| PP-F4-FE-002 | FE | Tutor: subir receta externa y revisión lado a lado | MUST | M4.1 | IMPLEMENTADO | | | Vista UploadExternalPrescriptionPage con dropzone, stepper, visor de imagen y atestación |
| PP-F4-FE-003 | FE | Tutor: mis recetas | MUST | M4.1 | IMPLEMENTADO | | | Vista GuardianPrescriptionsPage con tarjetas, filtros, visor de detalle y archivado |
| PP-F4-FE-004 | FE | Tutor: wizard de cotización | MUST | M4.2 | IMPLEMENTADO | | | Wizard de cotización en 3 pasos con bloqueo de controlados y selección de farmacias |
| PP-F4-FE-005 | FE | Tutor: tablero de cotizaciones (SSE) | MUST | M4.2 | IMPLEMENTADO | | | Tablero QuoteBoardView con SSE, comparación de precios, sustituciones y selección de oferta |
| PP-F4-FE-006 | FE | Tutor: pedidos | MUST | M4.2 | IMPLEMENTADO | | | Vistas OrderListView y OrderDetailView con timeline, cancelación y datos de contacto |
| PP-F4-FE-007 | FE | Portal: bandeja y formulario de cotización | MUST | M4.2 | IMPLEMENTADO | | | PharmacyInboxView con tabs de estado, visor de receta y modal de envío de cotización |
| PP-F4-FE-008 | FE | Portal: pedidos y dispensación | MUST | M4.2 | IMPLEMENTADO | | | PharmacyOrdersView y PharmacyOrderDetailView con transiciones y registro de lote/vence |
| PP-F4-FE-009 | FE | Portal: configuración y mapeo | MUST | M4.2 | IMPLEMENTADO | | | PharmacySettingsView con toggle de integración, áreas de entrega y mapeo de productos |
| PP-F4-FE-010 | FE | Portal: integración, credenciales, webhooks, métricas | MUST | M4.3 | IMPLEMENTADO | | | PharmacySettingsView con 5 pestañas completas: modalidad, catálogo, credenciales/sandbox, webhooks y métricas/SLA |
| PP-F4-FE-011 | FE | Administración F4 | MUST | M4.3 | IMPLEMENTADO | | | AdminPartnersPage, AdminRegulatoryPage, AdminAIPage y AdminOCRPage integrados en router admin y AdminLayout |
| PP-F4-FE-012 | FE | Verificación pública de receta | MUST | M4.0 | IMPLEMENTADO | | | Vista pública VerifyPrescriptionPage con cotejo criptográfico de hash |
| PP-F4-FE-013 | FE | Sitio de desarrolladores | SHOULD | M4.3 | IMPLEMENTADO | | | Sitio estático en developers/ con Scalar, guías de integración y validación en workflow docs.yml |
| PP-F4-OCR-001 | OCR | Pipeline y estados | MUST | M4.1 | IMPLEMENTADO | | | Pipeline secuencial con estados queued/processing/done/failed/reupload_requested |
| PP-F4-OCR-002 | OCR | Preprocesado y calidad de imagen | MUST | M4.1 | IMPLEMENTADO | | | Preprocesador binarización, deskew, estimación de blur Laplacian y auto-rotación |
| PP-F4-OCR-003 | OCR | Extractor Claude con salida estructurada y manejo de respuesta | MUST | M4.1 | IMPLEMENTADO | | | Extractor Claude con tools structured output, validación de schemas y manejo de refusals |
| PP-F4-OCR-004 | OCR | Extractor de respaldo (Tesseract) | SHOULD | M4.1 | IMPLEMENTADO | | | Extractor Tesseract fallback implementado con regex parsing para cabecera y posología |
| PP-F4-OCR-005 | OCR | Emparejamiento con catálogo | MUST | M4.1 | IMPLEMENTADO | | | Matcher fuzzy con similitud trigram y búsqueda sobre catálogo ref.medications |
| PP-F4-OCR-006 | OCR | Umbrales y reglas de revisión | MUST | M4.1 | IMPLEMENTADO | | | Cálculo de confianza global y por campo (verde >=0.85, ámbar >=0.60, rojo <0.60) |
| PP-F4-OCR-007 | OCR | Dataset y evaluación continua con gate | MUST | M4.1 | IMPLEMENTADO | | | Dataset sintético v1 (65 muestras) y subcomando worker ocr-eval con gates de F1 |
| PP-F4-OCR-008 | OCR | Presupuesto y control de coste | MUST | M4.1 | IMPLEMENTADO | | | Control de presupuesto diario y mensual en ocr.budget con corte 503 AI_BUDGET_EXCEEDED |
| PP-F4-OCR-009 | OCR | Privacidad y proveedor | MUST | M4.1 | IMPLEMENTADO | | | Zero-data-retention con Anthropic API y minimización estricta sin PII del tutor enviada |
| PP-F4-OCR-010 | OCR | Métricas de precisión en producción | MUST | M4.1 | IMPLEMENTADO | | | Registro de latencia, tokens, costes y diffs de corrección humana en ocr.field_metrics |
| PP-F4-SEC-001 | SEC | Autorización F4 y separación de audiencias | MUST | M4.2 | IMPLEMENTADO | | | Matriz de autorización SEC-001 implementada y validada en TestRoutes_AuthzMatrix |
| PP-F4-SEC-002 | SEC | Autenticación de partners | MUST | M4.3 | IMPLEMENTADO | | | Separación estricta de audiencias: token de usuario rechazado en /partner/v1 y token de partner rechazado en /api/v1 |
| PP-F4-SEC-003 | SEC | Webhooks: firma y replay | MUST | M4.3 | IMPLEMENTADO | | | Verificación HMAC-SHA256 constante en tiempo, rotación sin downtime y rechazo de timestamps con deriva > 300 s |
| PP-F4-SEC-004 | SEC | Egress y anti-SSRF | MUST | M4.3 | IMPLEMENTADO | | | Resolución DNS validada contra CIDRs privados, loopback y link-local antes de cada conexión HTTP externa |
| PP-F4-SEC-005 | SEC | Minimización y auditoría de envíos a partners | MUST | M4.2 | IMPLEMENTADO | | | Minimización de datos verificada con consentimientos de tutor y auditoría en BD |
| PP-F4-SEC-006 | SEC | Integridad y fraude de recetas | MUST | M4.0 | IMPLEMENTADO | | | Integridad criptográfica SHA-256 sobre eventos en orden canónico RFC 8785 |
| PP-F4-SEC-007 | SEC | Cumplimiento regulatorio | MUST | M4.0 | IMPLEMENTADO | | | Cumplimiento regulatorio, bloqueo de refills para controlados, expiración worker |
| PP-F4-SEC-008 | SEC | Proveedor de IA y datos | MUST | M4.1 | IMPLEMENTADO | | | Auditoría de egress con EgressClient, rate limiting y aislamiento de endpoints OCR |
| PP-F4-SEC-009 | SEC | Threat model F4 | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-SEC-010 | SEC | Prevención de abuso | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-INF-001 | INF | Borde B2B, sandbox, mTLS | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-INF-002 | INF | Egress proxy y reglas de red | MUST | M4.3 | IMPLEMENTADO | | | Estructura infra/egress/allowlist.yaml, auditoría de peticiones salientes y aislamiento de red en compose |
| PP-F4-INF-003 | INF | Observabilidad F4 | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-INF-004 | INF | Alertas F4 | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-INF-005 | INF | Runbook F4 | MUST | M4.5 | PENDIENTE | | | |
| PP-F4-INF-006 | INF | Secretos por partner y de IA | MUST | M4.3 | IMPLEMENTADO | | | Manejo de secretos con prefijos partners/{org_id}/webhook_current y webhook_previous para rotación segura |
| PP-F4-INF-007 | INF | Publicación del sitio de desarrolladores | SHOULD | M4.3 | IMPLEMENTADO | | | Generación e inspección del portal de desarrolladores en CI mediante job developers-site en docs.yml |
| PP-F4-QA-001 | QA | Pruebas de saga e invariantes | MUST | M4.2 | IMPLEMENTADO | | | Pruebas unitarias de invariantes canónicas Q1–Q8, O1–O8 y test de concurrencia de selección |
| PP-F4-QA-002 | QA | Contratos: Partner API, checklist go-live, adaptadores | MUST | M4.3 | IMPLEMENTADO | | | Suite hermética de 10 escenarios automatizados en partner-sim checklist y validación de contrato OpenAPI |
| PP-F4-QA-003 | QA | Evaluación OCR en CI y mocks | MUST | M4.1 | IMPLEMENTADO | | | Harness de evaluación worker ocr-eval con mock extractor superando F1 >= 0.85 |
| PP-F4-QA-004 | QA | Pruebas de seguridad B2B y prompt injection | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-QA-005 | QA | E2E F4 | MUST | M4.5 | PENDIENTE | | | |
| PP-F4-QA-006 | QA | Validación con piloto y revisión legal | MUST | M4.5 | PENDIENTE | | | |
| PP-F4-QA-007 | QA | Gates F4 | MUST | M4.0 | IMPLEMENTADO | | | Cobertura domain >= 90% (90.4%), authz matrix y linters limpios |
