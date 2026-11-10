# Webhooks en PetPassport Partner API (ARQ-004, PTN-004, SEC-003)

PetPassport despacha notificaciones HTTP asíncronas en tiempo real hacia los sistemas de las farmacias y distribuidores asociados cuando ocurren eventos clave en el ciclo de cotización y pedidos.

---

## 1. Eventos Soportados

| Evento | Descripción | Payload |
|---|---|---|
| `quote_request.created` | Se abre una solicitud de cotización donde el partner fue invitado (ARQ-013 etapa 1). | ID, pet (especie, peso, edad), requested items, área, vigencia. Sin PII. |
| `order.placed` | El tutor aceptó la oferta y se adjudicó el pedido al partner (ARQ-013 etapa 2). | ID de orden, tutor (nombre, teléfono), dirección de entrega, ítems adjudicados. |
| `order.cancelled` | El pedido fue cancelado (por tutor, timeout o sistema). | ID de orden, motivo de cancelación. |
| `ping` | Evento de prueba emitido desde la configuración del portal para validar conectividad. | Mensaje de comprobación y timestamp. |

---

## 2. Cabeceras HTTP Recibidas

Cada petición `POST` enviada por PetPassport contiene las siguientes cabeceras:

* `Content-Type`: `application/json`
* `X-PP-Event`: Nombre del evento (ej. `quote_request.created`).
* `X-PP-Delivery-ID`: UUID único de la entrega.
* `X-PP-Timestamp`: Marca de tiempo Unix en segundos (ej. `1700000000`).
* `X-PP-Signature`: Firma criptográfica con formato `v1=<hex_signature>`. Si hay rotación activa de claves, contiene múltiples firmas separadas por coma (`v1=<sig1>,v1=<sig2>`).

---

## 3. Algoritmo de Verificación de Firma

1. Extraiga el timestamp de la cabecera `X-PP-Timestamp`.
2. Verifique la ventana anti-replay: el timestamp recibido no debe diferir del tiempo actual en más de **300 segundos** (`|now - timestamp| <= 300s`).
3. Construya el mensaje a firmar concatenando el timestamp en texto, un punto literal `.` y los bytes exactos del cuerpo en crudo (raw body):
   $$\text{signed\_data} = \text{timestamp} + "." + \text{raw\_body}$$
4. Calcule el digest `HMAC-SHA256` utilizando su clave secreta configurada (`whsec_...`).
5. Compare en tiempo constante (`timingSafeEqual`) el valor calculado con las firmas presentes en la cabecera `X-PP-Signature`.

---

## 4. Ejemplos de Implementación de Verificación

````carousel
```go
// Go (net/http)
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func verifyWebhook(r *http.Request, secret string, maxAge time.Duration) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	tsStr := r.Header.Get("X-PP-Timestamp")
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("timestamp inválido")
	}

	// 1. Anti-replay (ventana de 300s)
	now := time.Now().Unix()
	if math.Abs(float64(now-ts)) > maxAge.Seconds() {
		return nil, fmt.Errorf("timestamp expirado")
	}

	// 2. Calcular HMAC
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", ts)
	mac.Write(body)
	expectedSig := "v1=" + hex.EncodeToString(mac.Sum(nil))

	// 3. Comparación en tiempo constante
	sigHeader := r.Header.Get("X-PP-Signature")
	for _, sig := range strings.Split(sigHeader, ",") {
		if hmac.Equal([]byte(strings.TrimSpace(sig)), []byte(expectedSig)) {
			return body, nil
		}
	}
	return nil, fmt.Errorf("firma no coincide")
}
```
<!-- slide -->
```typescript
// Node.js (Express / TypeScript)
import crypto from "crypto";
import { Request, Response, NextFunction } from "express";

export function verifyPetPassportWebhook(secret: string, toleranceSeconds = 300) {
  return (req: Request, res: Response, next: NextFunction) => {
    const rawBody = (req as any).rawBody || req.body;
    const timestampStr = req.header("X-PP-Timestamp");
    const sigHeader = req.header("X-PP-Signature");

    if (!timestampStr || !sigHeader) {
      return res.status(401).json({ error: "Cabeceras de firma ausentes" });
    }

    const timestamp = parseInt(timestampStr, 10);
    const now = Math.floor(Date.now() / 1000);
    if (Math.abs(now - timestamp) > toleranceSeconds) {
      return res.status(401).json({ error: "Timestamp fuera de tolerancia" });
    }

    const payload = `${timestamp}.${typeof rawBody === "string" ? rawBody : rawBody.toString("utf8")}`;
    const hmac = crypto.createHmac("sha256", secret).update(payload).digest("hex");
    const expected = `v1=${hmac}`;

    const signatures = sigHeader.split(",").map((s) => s.trim());
    const valid = signatures.some((sig) => {
      if (sig.length !== expected.length) return false;
      return crypto.timingSafeEqual(Buffer.from(sig), Buffer.from(expected));
    });

    if (!valid) {
      return res.status(401).json({ error: "Firma inválida" });
    }
    next();
  };
}
```
<!-- slide -->
```python
# Python (FastAPI / Flask)
import hmac
import hashlib
import time

def verify_petpassport_webhook(raw_body: bytes, timestamp_str: str, signature_header: str, secret: str, max_age_seconds: int = 300) -> bool:
    try:
        timestamp = int(timestamp_str)
    except (ValueError, TypeError):
        return False

    now = int(time.time())
    if abs(now - timestamp) > max_age_seconds:
        return False  # Replay attack

    signed_payload = f"{timestamp}.".encode("utf-8") + raw_body
    computed_digest = hmac.new(secret.encode("utf-8"), signed_payload, hashlib.sha256).hexdigest()
    expected_sig = f"v1={computed_digest}"

    signatures = [s.strip() for s in signature_header.split(",")]
    return any(hmac.compare_digest(sig, expected_sig) for sig in signatures)
```
<!-- slide -->
```php
<?php
// PHP
function verify_petpassport_signature(string $rawBody, string $timestampStr, string $sigHeader, string $secret, int $maxAge = 300): bool {
    $timestamp = (int)$timestampStr;
    $now = time();

    if (abs($now - $timestamp) > $maxAge) {
        return false; // Timestamp expirado
    }

    $signedData = $timestamp . '.' . $rawBody;
    $computedSig = 'v1=' . hash_hmac('sha256', $signedData, $secret);

    $signatures = array_map('trim', explode(',', $sigHeader));
    foreach ($signatures as $sig) {
        if (hash_equals($computedSig, $sig)) {
            return true;
        }
    }
    return false;
}
?>
```
````

---

## 5. Política de Reintentos y DLQ (ARQ-004)

Si su endpoint responde con un código distinto a `2xx` o se agota el timeout (10 segundos), el motor de webhooks de PetPassport aplica reintentos automáticos con retroceso exponencial:

1. **Intento 1**: inmediato.
2. **Intento 2**: a los 1 minuto.
3. **Intento 3**: a los 5 minutos.
4. **Intento 4**: a los 30 minutos.
5. **Intento 5**: a las 2 horas.
6. **Intento 6**: a las 12 horas.
7. **Dead Letter Queue (DLQ)**: Tras 5 intentos fallidos, la entrega queda en estado `failed` y puede ser consultada y reintentada manualmente desde el portal del partner.
