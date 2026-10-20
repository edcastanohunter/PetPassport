# Tracking — Fase 4: Integración B2B y Automatización (Pharmacy & OCR)

> Fuente de verdad del estado. Editar el estado **solo aquí**. Validar con `scripts/specs-status.sh --check`.
> Estados: `PENDIENTE` `EN_DISENO` `EN_PROGRESO` `IMPLEMENTADO` `VERIFICADO` `BLOQUEADO` `DESCARTADO`.

| ID | Área | Requisito | Prio | Hito | Estado | Issue | PR | Notas |
|---|---|---|---|---|---|---|---|---|
| PP-F4-PRD-001 | PRD | Credencial profesional del veterinario con verificación | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-PRD-002 | PRD | Emitir receta firmada desde consulta o directa; PDF/QR | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-PRD-003 | PRD | Verificación pública de receta | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-PRD-004 | PRD | Revocar receta | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-PRD-005 | PRD | Subir receta externa y ver extracción | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-PRD-006 | PRD | Revisar y confirmar extracción lado a lado | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-PRD-007 | PRD | Mis recetas (tutor) | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-PRD-008 | PRD | Solicitar cotización a farmacias | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-PRD-009 | PRD | Tablero de cotizaciones en tiempo real y comparación | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-PRD-010 | PRD | Pedido, seguimiento y dispensación reflejada | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-PRD-011 | PRD | Sustituciones: aceptar/rechazar | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-PRD-012 | PRD | Portal: bandeja y formulario de cotización; validación de recetas externas | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-PRD-013 | PRD | Portal: pedidos y registro de dispensación | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-PRD-014 | PRD | Portal: catálogo/mapeo, zonas, modalidades, horarios | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-PRD-015 | PRD | Portal: credenciales API, webhooks, sandbox | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-PRD-016 | PRD | Portal: métricas y SLA | SHOULD | M4.5 | PENDIENTE | | | |
| PP-F4-PRD-017 | PRD | Veterinario: estado de recetas y avisos de sustitución | SHOULD | M4.2 | PENDIENTE | | | |
| PP-F4-PRD-018 | PRD | SuperAdmin: partners, reglas regulatorias, presupuesto IA, monitor | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-PRD-019 | PRD | Notificaciones del ciclo receta→pedido | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-PRD-020 | PRD | Sustancias controladas: emitir sí, pedir no | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-PRD-021 | PRD | Farmacias cercanas al solicitar cotización | SHOULD | M4.2 | PENDIENTE | | | |
| PP-F4-PRD-022 | PRD | Valoración de farmacia | COULD | M4.5 | PENDIENTE | | | |
| PP-F4-DOM-001 | DOM | Glosario F4 | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-DOM-002 | DOM | Context map F4 | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-DOM-003 | DOM | ProfessionalCredential en Identity | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-DOM-004 | DOM | Agregado Prescription: comandos e invariantes R1–R9 | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-DOM-005 | DOM | Value Objects de receta | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-DOM-006 | DOM | Eventos Prescription v1 | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-DOM-007 | DOM | Entidad Extraction y Confirmation.Diff | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-DOM-008 | DOM | Agregado QuoteRequest (saga): comandos e invariantes Q1–Q8 | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-DOM-009 | DOM | Agregado Order: transiciones e invariantes O1–O8 | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-DOM-010 | DOM | Eventos Pharmacy Orders v1 | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-DOM-011 | DOM | PartnerIntegration, ProductMapping, WebhookDelivery, PartnerCall | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-DOM-012 | DOM | Reglas regulatorias por país | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-DOM-013 | DOM | Puertos F4 | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-DOM-014 | DOM | Process managers y políticas F4 | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-DOM-015 | DOM | SLA por partner (proyección) | SHOULD | M4.5 | PENDIENTE | | | |
| PP-F4-ARQ-001 | ARQ | Módulos nuevos y reglas de dependencia | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-ARQ-002 | ARQ | Saga de cotización: orquestación, timeouts, idempotencia | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-ARQ-003 | ARQ | PartnerGateway y niveles portal/api/adapter | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-ARQ-004 | ARQ | Webhooks salientes firmados con reintentos | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-ARQ-005 | ARQ | Partner API entrante: auth, idempotencia, límites, alcance | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-ARQ-006 | ARQ | Egress: anti-SSRF, circuit breaker, proxy | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-ARQ-007 | ARQ | Simulador de partner | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-ARQ-008 | ARQ | Entorno sandbox | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-ARQ-009 | ARQ | Sitio de desarrolladores | SHOULD | M4.3 | PENDIENTE | | | |
| PP-F4-ARQ-010 | ARQ | Borde partners.<dominio> en Traefik y WAF | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-ARQ-011 | ARQ | PDF y verificación de recetas | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-ARQ-012 | ARQ | Pipeline OCR en worker (integración) | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-ARQ-013 | ARQ | Contratos de minimización de datos hacia partners | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-ARQ-014 | ARQ | Read models F4 | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-API-001 | API | Endpoints de credenciales | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-API-002 | API | Endpoints de recetas emitidas | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-API-003 | API | Endpoints de recetas externas y extracción | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-API-004 | API | Mis recetas y verificación pública | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-API-005 | API | Endpoints de cotización (tutor) y SSE | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-API-006 | API | Endpoints de pedidos (tutor) | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-API-007 | API | Endpoints del portal de farmacia | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-API-008 | API | Configuración de farmacia: integración, mapeo, credenciales, webhook | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-API-009 | API | Administración: partners, reglas, presupuesto IA, monitor OCR | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-API-010 | API | Catálogo de errores F4 | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-PTN-001 | PTN | Contrato partner/v1 y versionado | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-PTN-002 | PTN | Autenticación client credentials y sandbox | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-PTN-003 | PTN | Endpoints partner | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-PTN-004 | PTN | Webhooks: eventos, firma, idempotencia | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-PTN-005 | PTN | Sandbox y simulador para partners | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-PTN-006 | PTN | Límites por partner | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-PTN-007 | PTN | Onboarding y checklist go-live | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-PTN-008 | PTN | SLA y métricas para partners | SHOULD | M4.5 | PENDIENTE | | | |
| PP-F4-DAT-001 | DAT | DDL read_prescriptions | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-DAT-002 | DAT | DDL read_pharmacyorders | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-DAT-003 | DAT | DDL credenciales, partner_integrations, mapeos, webhooks, partner_calls | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-DAT-004 | DAT | DDL ocr: uso, presupuesto, métricas, evaluación | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-DAT-005 | DAT | ref.regulatory_rules con semilla CO | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-DAT-006 | DAT | Retención de imágenes, payloads y PII de pedidos | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-FE-001 | FE | Veterinario: credencial, emitir receta, mis recetas | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-FE-002 | FE | Tutor: subir receta externa y revisión lado a lado | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-FE-003 | FE | Tutor: mis recetas | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-FE-004 | FE | Tutor: wizard de cotización | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-FE-005 | FE | Tutor: tablero de cotizaciones (SSE) | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-FE-006 | FE | Tutor: pedidos | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-FE-007 | FE | Portal: bandeja y formulario de cotización | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-FE-008 | FE | Portal: pedidos y dispensación | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-FE-009 | FE | Portal: configuración y mapeo | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-FE-010 | FE | Portal: integración, credenciales, webhooks, métricas | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-FE-011 | FE | Administración F4 | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-FE-012 | FE | Verificación pública de receta | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-FE-013 | FE | Sitio de desarrolladores | SHOULD | M4.3 | PENDIENTE | | | |
| PP-F4-OCR-001 | OCR | Pipeline y estados | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-OCR-002 | OCR | Preprocesado y calidad de imagen | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-OCR-003 | OCR | Extractor Claude con salida estructurada y manejo de respuesta | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-OCR-004 | OCR | Extractor de respaldo (Tesseract) | SHOULD | M4.1 | PENDIENTE | | | |
| PP-F4-OCR-005 | OCR | Emparejamiento con catálogo | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-OCR-006 | OCR | Umbrales y reglas de revisión | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-OCR-007 | OCR | Dataset y evaluación continua con gate | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-OCR-008 | OCR | Presupuesto y control de coste | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-OCR-009 | OCR | Privacidad y proveedor | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-OCR-010 | OCR | Métricas de precisión en producción | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-SEC-001 | SEC | Autorización F4 y separación de audiencias | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-SEC-002 | SEC | Autenticación de partners | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-SEC-003 | SEC | Webhooks: firma y replay | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-SEC-004 | SEC | Egress y anti-SSRF | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-SEC-005 | SEC | Minimización y auditoría de envíos a partners | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-SEC-006 | SEC | Integridad y fraude de recetas | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-SEC-007 | SEC | Cumplimiento regulatorio | MUST | M4.0 | PENDIENTE | | | |
| PP-F4-SEC-008 | SEC | Proveedor de IA y datos | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-SEC-009 | SEC | Threat model F4 | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-SEC-010 | SEC | Prevención de abuso | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-INF-001 | INF | Borde B2B, sandbox, mTLS | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-INF-002 | INF | Egress proxy y reglas de red | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-INF-003 | INF | Observabilidad F4 | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-INF-004 | INF | Alertas F4 | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-INF-005 | INF | Runbook F4 | MUST | M4.5 | PENDIENTE | | | |
| PP-F4-INF-006 | INF | Secretos por partner y de IA | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-INF-007 | INF | Publicación del sitio de desarrolladores | SHOULD | M4.3 | PENDIENTE | | | |
| PP-F4-QA-001 | QA | Pruebas de saga e invariantes | MUST | M4.2 | PENDIENTE | | | |
| PP-F4-QA-002 | QA | Contratos: Partner API, checklist go-live, adaptadores | MUST | M4.3 | PENDIENTE | | | |
| PP-F4-QA-003 | QA | Evaluación OCR en CI y mocks | MUST | M4.1 | PENDIENTE | | | |
| PP-F4-QA-004 | QA | Pruebas de seguridad B2B y prompt injection | MUST | M4.4 | PENDIENTE | | | |
| PP-F4-QA-005 | QA | E2E F4 | MUST | M4.5 | PENDIENTE | | | |
| PP-F4-QA-006 | QA | Validación con piloto y revisión legal | MUST | M4.5 | PENDIENTE | | | |
| PP-F4-QA-007 | QA | Gates F4 | MUST | M4.0 | PENDIENTE | | | |
