# F5 · 06 — Frontend

Nuevas *features*: `pricing`, `billing`, `privacy`, `support`, `status`, `channels`, `admin-billing`, `admin-metrics`.

**[PP-F5-FE-001] (MUST)** *Pricing* público (`/pricing`): comparativa por tipo (clínica/farmacia/tutor), selector mensual/anual con ahorro, moneda por país (detectada, cambiable), FAQ, CTA "Probar 30 días"; contenido desde `GET /plans` (nunca precios en código).

**[PP-F5-FE-002] (MUST)** *Prueba y upgrade*: al verificar la organización, pantalla "Tu prueba Pro empieza hoy" con cuenta atrás en el panel; banners T-7/T-1; al vencer, pantalla de bloqueo suave (lectura completa, acciones de escritura abren modal "Elige tu plan"); flujo de contratación en 3 pasos (plan y asientos → datos fiscales → pago) con vista previa de importe y prorrateo.

**[PP-F5-FE-003] (MUST)** *Facturación* (`/org/:orgId/settings/billing`): resumen (plan, estado con badge, próximo cargo, asientos usados/contratados con barra), cambiar plan/ciclo/asientos (con previsualización), método de pago (widget del proveedor embebido o redirección PSE; nunca formulario propio de tarjeta), datos fiscales, facturas (tabla con PDF/XML y estado DIAN), pagos, cancelar (con motivo y aviso de fecha efectiva), reactivar; estado `grace`/`restricted` con banner persistente y botón "Reintentar cobro".

**[PP-F5-FE-004] (MUST)** *Límites en contexto*: `PLAN_LIMIT_REACHED` → modal con el límite, uso, y acciones (añadir asiento / subir plan / esperar) sin perder el formulario; `SUBSCRIPTION_RESTRICTED` → modal con enlace a facturación; indicadores de uso en ajustes (barras al 80 % en ámbar, 100 % en rojo) y en el panel admin de clínica/farmacia.

**[PP-F5-FE-005] (MUST)** *Admin de facturación* (`/admin/billing`): cuentas (búsqueda, estado, plan, país, MRR), detalle con suscripciones, cargos, pagos, facturas (estado DIAN con reintento), discrepancias (resolver con nota), ajustes (extender prueba, crédito, cambio de plan, pago manual, reembolso) con motivo; planes (crear versión, publicar con fecha efectiva, retirar); cupones (COULD).

**[PP-F5-FE-006] (MUST)** *Privacidad* (`/app/settings/privacy`, `/org/:orgId/settings/privacy`): "Descargar mis datos" (explicación, re-autenticación, estado de la solicitud, enlace cuando esté listo), "Eliminar mi cuenta" (consecuencias con lista de mascotas y acción sugerida de transferencia, razones bloqueantes, confirmación por texto, gracia de 7 días con botón cancelar), "Accesos a mis datos" (soporte y clínicas), "Accesos de soporte" (concesiones activas, revocar).

**[PP-F5-FE-007] (MUST)** *Ayuda y soporte*: centro de ayuda público (`help.<dominio>`, estático, buscable, por rol); en la app botón "Ayuda" con artículos sugeridos por ruta y "Contactar soporte" (formulario con categoría, descripción, adjuntos, org); `/app/support` lista de tickets con hilo de mensajes, estado, SLA visible, cerrar y valorar.

**[PP-F5-FE-008] (MUST)** *Impersonación*: para el usuario, pantalla de concesión (alcance, datos clínicos, duración, ticket) y revocación; para el agente (`/support`, SPA de soporte o sección en admin con audiencia propia), buscador de usuarios (datos mínimos), inicio de sesión de impersonación solo con concesión vigente, **banner fijo rojo** "Viendo como <nombre> · solo lectura · expira en mm:ss · Salir", bloqueo visual de acciones de escritura si `read`, ocultación de secciones clínicas si no permitido.

**[PP-F5-FE-009] (MUST)** *Estado del servicio*: banner global cuando `status.json` reporta incidente o mantenimiento (con enlace a la status page); página `/status` embebida; en errores 5xx/503 persistentes, mensaje que enlaza al estado.

**[PP-F5-FE-010] (SHOULD)** *Teléfono y canales*: verificación por OTP (input de 6 dígitos, reenvío con cuenta atrás), preferencias por canal y categoría (SMS/WhatsApp solo si verificado), aviso de coste para clínicas (recordatorios por SMS consumen cuota), gestión de "STOP" (estado opt-out con cómo reactivar).

**[PP-F5-FE-011] (MUST)** *Métricas de negocio* (`/admin/metrics`): KPIs (MRR, ARR, cuentas activas, churn, conversión de prueba, cobros fallidos/recuperados), series y desgloses por plan/país, embudo de prueba, soporte (SLA, CSAT); exportación CSV; gráficos según `dataviz`.
