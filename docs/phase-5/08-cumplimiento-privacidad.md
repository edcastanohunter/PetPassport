# F5 · 08 — Cumplimiento y privacidad

Marco: ADR-023. Área `CMP`. Normativa de referencia: ley de protección de datos del país operativo (Colombia: Ley 1581/2012 y decretos), diseño compatible con GDPR para expansión; obligaciones fiscales (DIAN) y de conservación de documentos comerciales.

**[PP-F5-CMP-001] (MUST)** Registro de actividades de tratamiento (`privacy.processing_activities`, exportable): por cada tratamiento — finalidad, categorías de datos y titulares, base legal, encargados, transferencias (proveedor de IA, email, SMS, pagos, facturación, cloud), plazos de conservación, medidas; revisado por el responsable de privacidad en cada fase; versión pública resumida en la política de privacidad.

**[PP-F5-CMP-002] (MUST)** Evaluación de impacto (DPIA) para los tratamientos de riesgo: historial clínico vinculado a personas, OCR de recetas con proveedor externo, compartición con farmacias, impersonación de soporte, mensajería; riesgos, medidas, decisión; actualizada cuando cambie un tratamiento; plantilla en `docs/phase-5/compliance/dpia-template.md`.

**[PP-F5-CMP-003] (MUST)** Política de retención por clase de dato (tabla en ARQ-009) aprobada y automatizada; informe de cada ejecución; excepciones (`legal_hold`) documentadas; revisión anual.

**[PP-F5-CMP-004] (MUST)** Derechos del titular y notificaciones: procedimientos (exportación, supresión, rectificación, oposición a canales, consulta de accesos) con plazos legales (CO: consultas 10 días hábiles, reclamos 15), canal `privacidad@<dominio>` además de la app; registro de solicitudes y respuestas; procedimiento de **notificación de incidentes de seguridad con datos personales** (autoridad y titulares, plazos, plantilla), integrado con OPS-006; verificación de identidad para solicitudes fuera de la app.

**[PP-F5-CMP-005] (MUST)** Encargados y transferencias: inventario de proveedores con datos personales (cloud, Keycloak hosting si aplica, email, SMS/WhatsApp, IA, pagos, facturación, helpdesk, status page, observabilidad) con contrato/DPA firmado, ubicación de datos, subencargados, fecha de revisión; cláusulas para transferencias internacionales; evaluación anual; `docs/phase-5/compliance/processors.md`.

**[PP-F5-CMP-006] (MUST)** Revisión de accesos trimestral: cuentas `super_admin`, `support_agent`, accesos a cloud/K8s/BD/KMS/secret manager, claves de API de proveedores; principio de mínimo privilegio; baja inmediata al salir del equipo (checklist de offboarding); MFA obligatoria en todas las consolas; registro de la revisión con firmas.

**[PP-F5-CMP-007] (MUST)** Auditoría exportable: `audit_log` + accesos clínicos + accesos de soporte + cambios de facturación + DSR, consultables por SuperAdmin con filtros y exportables (CSV/JSON) para auditorías externas o requerimientos; integridad: hash encadenado diario de las particiones de `audit_log` almacenado en object storage con *object lock* (detecta manipulación).

**[PP-F5-CMP-008] (MUST)** Textos legales y consentimientos: términos por tipo de cliente (tutor, clínica, farmacia/partner), acuerdo de tratamiento de datos para organizaciones (roles responsable/encargado según el flujo: la clínica es responsable de los datos de sus pacientes que registra; PetPassport encargado + responsable de la plataforma — asesoría legal lo define), política de privacidad, política de cookies (solo técnicas + RUM con consentimiento), SLA; versionado (F1 PRD-020) con re-aceptación; revisión por asesor legal antes de M5.2 y M5.3.

**[PP-F5-CMP-009] (MUST)** Cumplimiento fiscal: habilitación como facturador electrónico ante DIAN a través del proveedor, resolución de numeración, pruebas en entorno de habilitación, conservación 10 años (object lock), notas crédito, reporte de contingencia; IVA y retenciones según régimen del cliente (campo en `LegalEntity`, validado por contador); conciliación contable mensual exportable (CSV de cargos, pagos, facturas, notas).

**[PP-F5-CMP-010] (SHOULD)** Marco de controles (preparación para ISO 27001 / SOC 2 en F6): mapa de controles existentes (acceso, cambio, continuidad, incidentes, proveedores, desarrollo seguro, registro) con evidencia automatizada donde sea posible (exportes de CI, revisiones de acceso, simulacros, postmortems) en `docs/phase-5/compliance/controls.md`; auditoría interna anual; *gap analysis* al cierre de F5.

**[PP-F5-CMP-011] (MUST)** Consentimiento de telemetría y cookies: banner mínimo (solo si se activan RUM/analítica con identificadores), preferencia persistida, RUM anonimizado sin consentimiento (sin ids), analítica de producto solo con consentimiento; sin terceros de publicidad.
