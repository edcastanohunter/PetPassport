# Guía de Onboarding y Checklist Go-Live (PTN-007, QA-002)

Para autorizar la emisión de credenciales en producción (`live`), todos los socios comerciales B2B (farmacias asociadas) deben completar satisfactoriamente la verificación automatizada contra la Partner API mediante la herramienta `partner-sim`.

## Pasos Previos de Onboarding

1. **Organización Verificada:** La farmacia debe contar con registro legal y aprobación de SuperAdmin en la plataforma PetPassport.
2. **Nivel de Integración Asignado:** SuperAdmin asigna el nivel `api` o `adapter` a la organización.
3. **Credenciales Sandbox:** Generadas en el Portal de Farmacia (`/pharmacies/{orgId}/settings` > Credenciales API) o vía `POST /pharmacies/{orgId}/api-credentials/sandbox`.
4. **Endpoint de Webhook:** Registrar la URL HTTPS pública de su receptor de webhooks y configurar el secreto HMAC.

---

## Ejecución del Simulador (`partner-sim`)

El simulador oficial `partner-sim` evalúa de forma hermética y end-to-end el cumplimiento del contrato técnico.

### Instalación / Compilación

```bash
cd backend
go build -o partner-sim ./cmd/partner-sim
```

### Ejecución de la Checklist Automatizada

```bash
./partner-sim checklist \
  --base-url "https://api.sandbox.petpassport.local" \
  --partner-token "eyJhbGciOi..." \
  --user-token "eyJhbGciOi..." \
  --webhook-secret "whsec_..."
```

---

## Matriz de Escenarios Evaluados (10/10)

| Nº | Escenario | Especificación | Descripción del Caso de Prueba |
|:---|:---|:---|:---|
| **1** | Separación de audiencias y tokens B2B | `ARQ-005`, `SEC-002` | Verifica que `/partner/v1` rechace peticiones anónimas (`401`) y tokens de usuario final (`aud=petpassport-api`). Verifica que tokens de partner (`aud=petpassport-partner-api`) sean rechazados en `/api/v1`. |
| **2** | Exploración del catálogo estándar | `PTN-003` | Consulta `GET /partner/v1/catalog/medications` con paginación y verifica la estructura canónica de medicamentos. |
| **3** | Mapeo individual de equivalencia SKU | `PTN-003` | Ejecuta `PUT /partner/v1/product-mappings/{catalogId}` asociando el SKU local de la farmacia con el catálogo maestro. |
| **4** | Carga masiva idempotente de mapeos | `PTN-003` | Ejecuta `POST /partner/v1/product-mappings/bulk` con cabecera `Idempotency-Key` para registrar catálogos en lote. |
| **5** | Bandeja de solicitudes de cotización | `PTN-001` | Consulta `GET /partner/v1/quote-requests` validando la minimización estricta de datos (sin PII de tutores expuesta). |
| **6** | Envío de oferta de cotización | `PTN-003` | Envía `POST /partner/v1/quote-requests/{id}/quotes` con precios unitarios, tiempos de entrega y disponibilidad. |
| **7** | Propuesta de sustitución bioequivalente | `PRD-011`, `PTN-003` | Envía cotización con propuesta de sustitución genérica y justificación farmacéutica explícita. |
| **8** | Rechazo formal de cotización | `PTN-003` | Envía `POST /partner/v1/quote-requests/{id}/decline` indicando motivo operativo (ej. `out_of_stock`). |
| **9** | Ciclo de vida y dispensación de pedido | `DOM-009`, `PTN-003` | Ejecuta la secuencia de transiciones (`confirmed` → `preparing` → `ready` → `out_for_delivery` → `delivered`) incluyendo número de lote y fecha de expiración. |
| **10** | Validación criptográfica de webhooks | `PTN-004`, `SEC-003` | Valida firmas HMAC-SHA256, tolerancia a rotación de secretos (doble firma), ventana anti-replay (300 s) y rechazo de payloads alterados. |

---

## Criterio de Aprobación

El resultado de la ejecución debe ser **10/10 PASS**:
```text
================================================================================
             PETPASSPORT PARTNER API GO-LIVE CHECKLIST (QA-002)                 
================================================================================
[PASS] Escenario  1: Separación de audiencias y tokens B2B (ARQ-005, SEC-002)
[PASS] Escenario  2: Exploración de catálogo estándar (PTN-003)
[PASS] Escenario  3: Mapeo individual de equivalencia SKU (PTN-003)
[PASS] Escenario  4: Carga masiva idempotente de mapeos (PTN-003)
[PASS] Escenario  5: Bandeja de solicitudes de cotización (PTN-001)
[PASS] Escenario  6: Envío de oferta de cotización con precios (PTN-003)
[PASS] Escenario  7: Propuesta de sustitución equivalente (PRD-011, PTN-003)
[PASS] Escenario  8: Rechazo formal de solicitud de cotización (PTN-003)
[PASS] Escenario  9: Transiciones de pedido y dispensación (DOM-009, PTN-003)
[PASS] Escenario 10: Firmas HMAC-SHA256, rotación y anti-replay (PTN-004, SEC-003)
--------------------------------------------------------------------------------
Resultado: 10/10 escenarios superados exitosamente.
ESTADO: GO-LIVE APROBADO - Integración cumple el 100% de los requisitos técnicos.
```

Una vez aprobado, el SuperAdmin o el propio portal habilitará el botón para generar las credenciales de producción (`client_id` y `client_secret` en el realm `petpassport`).
