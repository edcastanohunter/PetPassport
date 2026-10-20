# F5 · 01 — Especificación de producto

## 1. Personas (cambios)

| Persona | Novedades F5 |
|---|---|
| **Admin de clínica / farmacia** | Contrata plan, gestiona asientos, método de pago, facturas; ve uso y límites; abre tickets. |
| **Titular de facturación** (nuevo rol `billing_admin`, por defecto el admin) | Datos fiscales, cuenta que agrupa varias organizaciones (cadenas). |
| **Tutor** | Exporta/elimina sus datos; verifica teléfono; recibe SMS/WhatsApp. |
| **Soporte** (nuevo rol global `support_agent`) | Atiende tickets, impersona con consentimiento, no ve datos clínicos salvo autorización. |
| **SuperAdmin** | Suscripciones, ajustes, reembolsos, cupones, métricas de negocio, DSR, cumplimiento. |

## 2. Épicas

| Épica | Historias |
|---|---|
| E25 Planes y contratación | PRD-001, 002, 003, 007, 008, 009, 010, 021, 022 |
| E26 Cobro y facturación | PRD-004, 005, 006, 011 |
| E27 Privacidad y derechos | PRD-012, 013, 020 |
| E28 Soporte y operación visible | PRD-014, 015, 016, 018, 019 |
| E29 Canales | PRD-017 |

## 3. Planes (estructura; precios finales los fija negocio)

| Plan | Para | Base de cobro | Incluye | Límites |
|---|---|---|---|---|
| **Tutor** | personas | gratuito | todo lo de F1–F4 | 10 mascotas activas, 10 enlaces compartidos activos |
| **Clínica Starter** | 1–2 profesionales | fijo/mes (≈ 25–40 USD eq.) | agenda, consultas, carnet, recetas, cotizaciones | 2 asientos profesionales, sin cohortes ni SMS |
| **Clínica Pro** | clínicas | por profesional/mes (≈ 15–25 USD eq.) | + cohortes y campañas, directorio destacado, SMS/WhatsApp (cuota), soporte prioritario | asientos = profesionales activos (recepcionistas no cuentan) |
| **Clínica Enterprise** | cadenas (≥ 5 sedes) | contrato anual, factura manual | + cuenta multi-organización, SLA 99,9 % con créditos, exportaciones, soporte dedicado | negociado |
| **Farmacia Basic** | farmacias nivel portal | gratuito | portal de cotizaciones y pedidos | 50 cotizaciones respondidas/mes |
| **Farmacia Pro** | farmacias con volumen o API | por tramos de cotizaciones/mes (≈ 30–150 USD eq.) | + API de partner, webhooks, métricas, directorio | tramos 200 / 1.000 / ilimitado |

Prueba de 30 días (Pro) sin tarjeta; anual con 2 meses de descuento; impuestos según país; precios en COP (con USD informativo).

## 4. Historias y criterios de aceptación

### E25 — Planes y contratación

**[PP-F5-PRD-001] (MUST)** Como visitante quiero ver los planes y precios para decidir.
- Página pública `/pricing` con comparativa, moneda por país, preguntas frecuentes, CTA a registro/prueba; los planes vienen del catálogo versionado (no texto estático).

**[PP-F5-PRD-002] (MUST)** Como admin de clínica quiero probar el plan Pro 30 días sin tarjeta.
- Al registrar la organización (F1) se activa `trialing` con entitlements Pro; recordatorios T-7d/T-1d; al vencer sin método de pago pasa a Starter gratuito limitado? **No**: pasa a `trial_expired` con lectura completa y escritura restringida a lo que cubra Starter sin pago (no existe gratuito para clínicas) → `restricted` con gracia 7 días y aviso claro; contratar reactiva al instante.

**[PP-F5-PRD-003] (MUST)** Como admin quiero contratar, cambiar de plan y ajustar asientos sin hablar con nadie.
- Checkout con datos fiscales, método de pago, resumen de cargo (prorrateo) e impuestos; cambio inmediato de entitlements; downgrade al final del ciclo con validación (si hay más profesionales que asientos, debe retirar o pagar); confirmación por email con factura.

**[PP-F5-PRD-007] (MUST)** Como usuario quiero entender los límites de mi plan cuando los alcanzo.
- Mensajes `PLAN_LIMIT_REACHED` con qué límite, uso actual y acción (añadir asiento / subir plan / esperar al nuevo periodo); indicador de uso en ajustes; aviso al 80 % de cuotas de volumen.

**[PP-F5-PRD-008] (SHOULD)** Como cadena quiero una cuenta de facturación que agrupe varias clínicas con una factura y asientos compartidos.
- `BillingAccount` con N organizaciones; asientos a nivel de cuenta (pool) o por organización (config); invitación de organizaciones existentes a la cuenta con aceptación del admin de cada una.

**[PP-F5-PRD-009] (SHOULD)** Como cadena quiero plan Enterprise con factura manual (transferencia) y condiciones negociadas; SuperAdmin registra el contrato y los pagos; la suscripción no entra en dunning automático.

**[PP-F5-PRD-010] (MUST)** Como admin quiero ver el uso de mi organización (asientos, cotizaciones del mes, SMS, almacenamiento de adjuntos) con histórico.

**[PP-F5-PRD-021] (COULD)** Plan *Tutor Plus* (mascotas ilimitadas, enlaces ilimitados, exportación PDF completa, co-tutores) — decisión con datos de F3/F4.

**[PP-F5-PRD-022] (COULD)** Cupones y descuentos (porcentaje/importe, duración, límite de usos) aplicables en checkout; gestionados por SuperAdmin.

### E26 — Cobro y facturación

**[PP-F5-PRD-004] (MUST)** Como admin quiero pagar con tarjeta o PSE/transferencia y que el cobro sea recurrente.
- Métodos según proveedor del país (tarjeta, PSE en CO); tokenización en el proveedor (PetPassport nunca ve el PAN); cobro al inicio de cada ciclo; recibo por email; cambio de método sin interrumpir el servicio.

**[PP-F5-PRD-005] (MUST)** Como admin quiero facturas electrónicas válidas con mis datos fiscales.
- Factura emitida tras cada cobro (o al generar el cargo en manual) con NIT/CC, razón social, dirección, IVA; PDF y XML (CO: validada por DIAN con CUFE); historial descargable; nota crédito en reembolsos/ajustes.

**[PP-F5-PRD-006] (MUST)** Como admin quiero avisos claros si falla el pago, sin perder acceso de inmediato.
- Reintentos 3 en 14 días con emails (día 0, 3, 7, 12); `grace` sin restricciones 14 días; luego `restricted` (lectura completa, escritura bloqueada salvo pagar); banner persistente; reactivación inmediata al pagar; cancelación definitiva a los 60 días con exportación disponible.

**[PP-F5-PRD-011] (MUST)** Como SuperAdmin quiero gestionar suscripciones: ver estado y pagos, aplicar ajustes (extender prueba, cortesías, cambiar plan), registrar pagos manuales, emitir reembolsos/notas crédito, cancelar; todo con motivo y auditoría.

### E27 — Privacidad y derechos

**[PP-F5-PRD-012] (MUST)** Como tutor (o admin de organización) quiero exportar mis datos.
- Solicitud en ajustes → verificación (re-login reciente) → ZIP cifrado (JSON + PDFs) disponible 7 días por enlace de un solo uso; plazo ≤ 72 h (objetivo minutos); organización: datos de la org, miembros (sin datos personales de terceros no necesarios), pacientes vinculados solo los registros propios (custodia).

**[PP-F5-PRD-013] (MUST)** Como tutor quiero eliminar mi cuenta.
- Verificación, explicación de consecuencias (mascotas: transferir o quedarán sin tutor; recetas con dispensación y facturas se conservan el plazo legal anonimizadas), gracia de 7 días con cancelación por enlace; al ejecutarse: supresión criptográfica, baja en Keycloak, confirmación por email; organizaciones: el último admin debe transferir o cerrar la organización antes.

**[PP-F5-PRD-020] (MUST)** Textos legales por plan y país (términos de servicio por tipo de cliente, acuerdo de tratamiento para clínicas/farmacias como encargados/responsables, política de privacidad actualizada con canales y pagos) con versionado y re-aceptación cuando cambian.

### E28 — Soporte y operación visible

**[PP-F5-PRD-014] (MUST)** Como usuario quiero ayuda y soporte desde la app.
- Centro de ayuda público (artículos por rol, buscable); "Ayuda" en la app con artículos contextuales y formulario de ticket (categoría, descripción, adjuntos) → email de seguimiento; estados del ticket; SLA de primera respuesta por plan (Starter 2 días hábiles, Pro 8 h, Enterprise 2 h); satisfacción al cerrar.

**[PP-F5-PRD-015] (MUST)** Status page pública con componentes (app, API, partners API, notificaciones), incidentes con actualizaciones, mantenimientos programados, historial de disponibilidad; banner en la app durante incidentes.

**[PP-F5-PRD-016] (SHOULD)** SLA publicado por plan (Pro 99,5 %, Enterprise 99,9 %) con créditos automáticos (% de la cuota mensual) calculados desde la disponibilidad medida; política de mantenimiento.

**[PP-F5-PRD-018] (MUST)** Como agente de soporte quiero ver la app como el usuario para resolver su problema, con su consentimiento.
- El usuario autoriza desde el ticket (o desde ajustes) una sesión de 30 min; el agente ve un banner "Viendo como X"; solo lectura por defecto, escritura con autorización explícita; datos clínicos ocultos salvo que el usuario lo marque; todo auditado y visible para el usuario ("Accesos de soporte").

**[PP-F5-PRD-019] (MUST)** Métricas de negocio para SuperAdmin: MRR/ARR, nuevos, churn, upgrades/downgrades, trials y conversión, cobros fallidos, ingresos por plan/país, activación (F1), NPS/CSAT de soporte.

### E29 — Canales

**[PP-F5-PRD-017] (SHOULD)** Como tutor quiero recibir recordatorios por SMS/WhatsApp.
- Verificación de teléfono por OTP; opt-in por canal y categoría; plantillas aprobadas (WhatsApp); cuota mensual por plan de la clínica (los recordatorios de cita los paga la clínica; los de vacuna por defecto email/push, SMS opcional del tutor en Plus); baja por respuesta "STOP"; costes visibles en el uso de la clínica.

## 5. Reglas de negocio F5

| Regla | Detalle |
|---|---|
| RN-32 | La lectura nunca se bloquea por facturación; la escritura solo tras gracia y conciliación. |
| RN-33 | Entitlements derivan de la suscripción vigente; un fallo del proveedor de pagos no cambia entitlements. |
| RN-34 | Factura fiscal por cada cobro; nota crédito por cada reembolso; sin excepciones. |
| RN-35 | Asientos = profesionales (`veterinarian`) activos; recepcionistas y admins no profesionales no cuentan. |
| RN-36 | Supresión: PII del tutor irrecuperable; historia del animal y obligaciones legales (recetas dispensadas, facturas) se conservan el plazo legal, anonimizadas. |
| RN-37 | Impersonación solo con consentimiento vigente, con banner, auditada y visible para el usuario. |
| RN-38 | Mensajes SMS/WhatsApp solo con teléfono verificado y opt-in; contenido mínimo. |
| RN-39 | Todo cambio de precio se anuncia con ≥ 30 días y aplica en el siguiente ciclo. |

## 6. Métricas F5

- MRR, churn mensual (< 3 % objetivo), conversión de prueba (> 25 %), cobros fallidos recuperados (> 60 %).
- Facturas aceptadas por DIAN al primer intento (> 98 %).
- DSR: tiempo medio de exportación (< 1 h) y de supresión (gracia + < 24 h).
- Soporte: primera respuesta dentro de SLA (> 95 %), CSAT (> 4,5/5).
- Disponibilidad mensual y error budget; incidentes con postmortem en ≤ 5 días (100 %).
