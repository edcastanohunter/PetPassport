# ADR-025: Entitlements derivados de la suscripción, consultados por puerto; límites blandos con periodo de gracia; medición de uso por eventos

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F5

## Contexto

Los planes limitan asientos (profesionales), volumen (cotizaciones/mes) y capacidades (API de partner,
cohortes, directorio, SMS). Los límites deben aplicarse en el dominio sin acoplarlo a Billing ni castigar a un
cliente por un fallo de cobro transitorio.

## Decisión

1. **Puerto `Entitlements`** (definido en `shared/kernel`): `For(orgID) EntitlementSet{Plan, Seats{Limit,
   Used}, Features set, Quotas map[QuotaKey]{Limit, Used, Period}, Status active|grace|restricted}`; implementado
   por *Billing* sobre un read model (`billing.entitlements`) cacheado en Redis (60 s) e invalidado por eventos de
   suscripción y de uso.
2. **Aplicación en comandos**: cada comando que consume un límite declara su `QuotaKey`/`Feature`
   (`InviteMember` → `seats`, `RequestQuotes` → `quotes_per_month`, `CreateWebhook` → `feature:partner_api`).
   El handler consulta `Entitlements` **antes** de ejecutar; si excede → `PLAN_LIMIT_REACHED` (422) con
   detalle y enlace de *upgrade*; si la cuenta está `restricted` → `SUBSCRIPTION_RESTRICTED` (402) para
   comandos de escritura; la lectura nunca se bloquea.
3. **Límites blandos**: superar un límite de volumen permite un 10 % extra en el periodo con aviso; las
   cuotas duras son asientos y capacidades. `past_due` → `grace` 14 días sin restricciones; después
   `restricted`.
4. **Medición de uso**: proyección `billing.usage_counters` desde eventos de negocio (`QuoteRequested`,
   `MemberJoined/Removed`, `EncounterSigned`…) por organización y periodo; es la base de la facturación por
   volumen y de los paneles de uso; nunca se incrementa desde la API directamente.
5. **Tutores**: sin suscripción en F5; `EntitlementSet` por defecto (gratuito). Un plan *Plus* para tutores se
   evalúa con datos de uso (COULD, PRD).
6. **Gratuito para farmacias en nivel portal** hasta un volumen (p. ej. 50 cotizaciones/mes), luego plan Pro
   por tramos; la API de partner es capacidad de pago.

## Consecuencias

- Los contextos de negocio dependen de una interfaz pequeña; las pruebas usan un `FakeEntitlements`.
- Cambiar precios o límites es configuración de planes (`billing.plans`, versionada), no código.
- Hay que vigilar la latencia añadida (una lectura cacheada) y la consistencia tras *upgrades* (invalidación).

## Alternativas consideradas

- Comprobar límites en el frontend/gateway: fácil de eludir.
- Bloquear al primer impago: hostil para clínicas con tarjetas vencidas; se opta por gracia.
