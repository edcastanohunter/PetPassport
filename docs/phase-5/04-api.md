# F5 · 04 — API REST v1 (ampliación)

Convenciones de F1–F4 vigentes. Nuevo rol global `billing_admin` (por cuenta) y `support_agent` (realm). Nueva categoría de notificación `billing` (no desactivable).

## 1. Planes — **[PP-F5-API-001] (MUST)**

| Método y ruta | Authz | Respuesta |
|---|---|---|
| `GET /plans?kind=clinic\|pharmacy&country=` (público) | — | `{data: Plan[{code, version, name, pricing{model, currency, monthly, annual, tiers[]}, trial_days, seats{min,max}, quotas{…}, features[], effective_from}]}` |
| `GET /plans/{code}` (público) | — | plan vigente |
| `POST/PUT /admin/plans` · `POST /admin/plans/{code}/publish` · `/retire` | `super_admin` | versionado; `publish` exige `effective_from ≥ +30 d` si hay suscripciones activas en la versión anterior |

## 2. Cuenta de facturación — **[PP-F5-API-002] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /organizations/{org_id}/billing` | `billing:read` (org admin) | `{account?{id, legal_entity, country, currency, payment_method{kind, last4, expires_at}?, organizations[]}, subscription?{…}, entitlements{…}}` |
| `POST /billing-accounts` *(Idempotency-Key)* | org admin | `{legal_entity{kind, legal_name, tax_id{country, value, kind}, address, email, phone?, tax_regime?}, organization_id}` → `201 BillingAccount` |
| `PATCH /billing-accounts/{id}` *(If-Match)* | `billing:manage` | datos fiscales |
| `POST /billing-accounts/{id}/organizations` · `DELETE …/{org_id}` | `billing:manage` + aceptación del admin de la org (invitación) | cadenas |
| `POST /billing-accounts/{id}/close` | `billing:manage` | sin suscripción activa |

## 3. Suscripción — **[PP-F5-API-003] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /billing-accounts/{id}/subscriptions` · `GET /subscriptions/{id}` | `billing:read` | `Subscription{kind, plan{code, version, name}, seats?{purchased, used}, tier?, cycle, status, trial_ends_at?, current_period{start, end}, pending_change?, dunning?{grace_ends_at, next_attempt_at}, cancel_at_period_end, charges[…]}` |
| `POST /billing-accounts/{id}/subscriptions/preview` | `billing:manage` | `{kind, plan_code, seats?\|tier?, cycle, coupon?}` → `{lines[], subtotal, tax, total, currency, proration?, next_charge_at}` |
| `POST /billing-accounts/{id}/subscriptions` *(Idempotency-Key)* | `billing:manage` | mismo cuerpo → `201 Subscription` (+ cargo). `402 PAYMENT_METHOD_REQUIRED`, `422 SEATS_BELOW_USAGE` |
| `POST /subscriptions/{id}/change` *(If-Match)* | `billing:manage` | `{plan_code?, seats?\|tier?, cycle?}` → `200 Subscription` (upgrade inmediato / downgrade programado) |
| `POST /subscriptions/{id}/seats` *(If-Match)* | `billing:manage` | `{delta}` |
| `POST /subscriptions/{id}/cancel` | `billing:manage` | `{at_period_end: bool, reason?}` |
| `POST /subscriptions/{id}/resume` | `billing:manage` | revierte cancelación programada |
| `POST /subscriptions/{id}/retry-charge` | `billing:manage` | reintento inmediato tras cambiar método |

## 4. Métodos de pago y checkout — **[PP-F5-API-004] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `POST /billing-accounts/{id}/payment-methods/setup` | `billing:manage` | `{kind: card\|pse}` → `{provider, client_config{…}}` (datos para el widget/redirect del proveedor) |
| `POST /billing-accounts/{id}/payment-methods` | `billing:manage` | `{provider_token}` → `200 {kind, last4, expires_at}` |
| `DELETE /billing-accounts/{id}/payment-methods` | `billing:manage` | solo si no hay suscripción activa o es manual |
| `POST /webhooks/payments/{provider}` (público, firmado) | — | idempotente por `provider_event_id` |

## 5. Facturas y pagos — **[PP-F5-API-005] (MUST)**

| Método y ruta | Authz | Respuesta |
|---|---|---|
| `GET /billing-accounts/{id}/invoices?cursor` | `billing:read` | `{data: [{id, number, status, issued_at, total, currency, pdf_url, xml_url?, charge_id, credit_notes[]}]}` |
| `GET /invoices/{id}/pdf` · `/xml` | `billing:read` | `302` prefirmada |
| `GET /billing-accounts/{id}/payments?cursor` | `billing:read` | pagos y reembolsos |

## 6. Uso y entitlements — **[PP-F5-API-006] (MUST)**

| Método y ruta | Authz | Respuesta |
|---|---|---|
| `GET /organizations/{org_id}/entitlements` | miembro | `EntitlementSet{plan_code, status, seats{limit, used}, quotas{key: {limit, used, soft_limit, period_end}}, features[]}` |
| `GET /organizations/{org_id}/usage?from=&to=` | org admin | series por cuota y periodo |

Errores comunes en comandos de negocio: `422 PLAN_LIMIT_REACHED {quota, limit, used, period_end, upgrade_url}`, `402 SUBSCRIPTION_RESTRICTED {reason, grace_ends_at?, billing_url}`.

## 7. Administración de facturación — **[PP-F5-API-007] (MUST)**

`GET /admin/billing/accounts?q=&status=&plan=&country=` · `GET /admin/billing/accounts/{id}` (con pagos, facturas, discrepancias, auditoría) · `POST /admin/subscriptions/{id}/adjust` `{kind: extend_trial\|credit\|comp_plan\|change_plan, params, reason}` · `POST /admin/charges/{id}/manual-payment` `{amount, reference, reason}` · `POST /admin/charges/{id}/refund` `{amount, reason}` · `POST /admin/subscriptions/{id}/cancel` `{reason}` · `GET /admin/billing/discrepancies?status=` · `POST …/{id}/resolve` `{resolution, reason}` · `POST /admin/coupons` (COULD) · `GET /admin/billing/invoices?status=rejected` · `POST /admin/invoices/{id}/retry`.

## 8. Derechos del titular — **[PP-F5-API-008] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `POST /me/data-requests` *(Idempotency-Key)* | autenticado + re-auth ≤ 10 min (`auth_time`) | `{kind: export\|erasure}` → `201 {id, status, grace_ends_at?}`. `409 REQUEST_IN_PROGRESS`, `403 REAUTH_REQUIRED`, `409 ERASURE_BLOCKED {reasons: [last_admin_of_org, active_subscription]}` |
| `GET /me/data-requests` · `GET /me/data-requests/{id}` | autenticado | estado; export `ready` incluye `download_url` (un solo uso) |
| `POST /me/data-requests/{id}/cancel` | autenticado | solo `erasure` en gracia |
| `POST /organizations/{org_id}/data-requests` | org admin | `{kind: export}` |
| `GET /admin/data-requests?status=` · `POST …/{id}/reject` `{reason, legal_basis}` · `POST …/{id}/expedite` | `super_admin` | |
| `GET /me/support-access` · `POST /me/support-access` `{ticket_id?, scope: read\|write, clinical_allowed, minutes ≤ 30}` · `DELETE /me/support-access/{id}` | autenticado | concesiones de impersonación |
| `GET /me/access-log` | autenticado | accesos de soporte y de clínicas (F2 SEC-005 COULD → aquí MUST) |

## 9. Soporte — **[PP-F5-API-009] (MUST)**

`GET /help/articles?q=&role=` (público) · `POST /support/tickets` *(Idempotency-Key)* `{category, subject, body, organization_id?, attachment_keys[]}` · `GET /support/tickets?status=` · `GET /support/tickets/{id}` · `POST /support/tickets/{id}/messages` · `POST /support/tickets/{id}/close` · `POST /support/tickets/{id}/rate` `{score, comment?}`; agente: `GET /admin/support/tickets?status=&priority=&assignee=` · `POST …/{id}/assign` · `/reply` · `/resolve` · `POST /admin/support/impersonate` `{session_id}` → `{token, expires_at, scope}` (solo con concesión vigente) · `GET /admin/support/users?q=` (búsqueda con datos mínimos).

## 10. Teléfono y canales — **[PP-F5-API-010] (SHOULD)**

`POST /me/phone/verify` `{phone}` → OTP · `POST /me/phone/confirm` `{code}` · `PUT /me/notification-preferences` ampliado con `sms{…}`, `whatsapp{…}` · `POST /webhooks/messaging/{provider}` (inbound STOP, estados de entrega, firmado) · `GET /organizations/{org_id}/messaging/usage`.

## 11. Métricas de negocio — **[PP-F5-API-011] (MUST)**

`GET /admin/metrics/business?from=&to=&granularity=` → `{mrr{total, by_plan, by_country}, arr, accounts{trialing, active, grace, restricted, cancelled}, trials{started, converted, expired, conversion_rate}, churn{count, rate, mrr_lost}, upgrades, downgrades, failed_charges{count, recovered}, sla_credits, support{tickets, first_response_sla_pct, csat}}` · `GET /admin/metrics/business/export.csv`.

## 12. Catálogo de errores (ampliación) — **[PP-F5-API-012] (MUST)**

| `code` | HTTP | Cuándo |
|---|---|---|
| `PLAN_LIMIT_REACHED` | 422 | Cuota/asientos/capacidad del plan. |
| `SUBSCRIPTION_RESTRICTED` | 402 | Escritura con suscripción restringida o prueba vencida. |
| `PAYMENT_METHOD_REQUIRED` / `PAYMENT_METHOD_INVALID` | 402 / 422 | |
| `SEATS_BELOW_USAGE` | 422 | Reducir asientos por debajo de profesionales activos. |
| `PLAN_NOT_AVAILABLE` / `PLAN_CHANGE_PENDING` | 422 / 409 | |
| `TAX_ID_INVALID` / `LEGAL_ENTITY_INCOMPLETE` | 422 | |
| `ORGANIZATION_ALREADY_BILLED` | 409 | Org ya pertenece a otra cuenta. |
| `CHARGE_NOT_REFUNDABLE` / `REFUND_EXCEEDS` | 409 / 422 | |
| `INVOICE_REJECTED` | 422 | Motivo del proveedor fiscal (admin). |
| `REAUTH_REQUIRED` | 403 | DSR y acciones sensibles sin autenticación reciente. |
| `ERASURE_BLOCKED` | 409 | Razones bloqueantes. |
| `SUPPORT_ACCESS_NOT_GRANTED` / `SUPPORT_ACCESS_EXPIRED` | 403 / 410 | |
| `IMPERSONATION_READ_ONLY` / `IMPERSONATION_CLINICAL_DENIED` | 403 | |
| `PHONE_NOT_VERIFIED` / `OTP_INVALID` / `OTP_ATTEMPTS_EXCEEDED` | 422 / 422 / 429 | |
| `CHANNEL_OPTED_OUT` | 422 | STOP recibido. |
