# Vectores de Prueba de Firma HMAC-SHA256 (SEC-003, PTN-004)

Este documento proporciona la especificación formal y vectores de prueba canónicos para validar implementaciones del cliente receptor de webhooks de PetPassport Partner API.

## Algoritmo de Firma

PetPassport firma todos los webhooks salientes utilizando **HMAC-SHA256**.
La cabecera `X-PP-Signature` contiene la versión del esquema y el resumen hexadecimal calculado:

```http
X-PP-Timestamp: 1731171600
X-PP-Signature: v1=c8afcdae1d2b3a7823d59a5d193c47c6e1cb672703d7f1dc22a96a255faa7f63
```

### Cadena a firmar

El payload firmado se compone concatenando el timestamp en segundos UNIX, un carácter de punto (`.`), y los bytes crudos (raw) del cuerpo de la petición HTTP recibida:

```text
string_to_sign = timestamp + "." + raw_body
```

> **Importante:** Utilice los bytes crudos del cuerpo sin parsear ni volver a serializar JSON, para evitar discrepancias de orden de propiedades o espacios en blanco.

## Protección Anti-Replay

Para evitar ataques de repetición (replay attacks):
1. Obtenga el valor entero de la cabecera `X-PP-Timestamp`.
2. Compare con el tiempo actual del servidor (`now`):
   $$\| \text{now} - \text{timestamp} \| \le 300\text{ s}$$
3. Si la diferencia es superior a 300 segundos (5 minutos) en el pasado o en el futuro, rechace la petición con código HTTP `400 Bad Request` o `401 Unauthorized`.

---

## Vectores Canónicos de Prueba

### Vector 1: Solicitud de cotización (`quote_request.created`)

- **Secreto compartido (`secret`)**:
  `whsec_test_secret_32_bytes_long_12345`
- **Timestamp (`X-PP-Timestamp`)**:
  `1731171600` (correspondiente a `2024-11-09T17:00:00Z`)
- **Cuerpo HTTP crudo (`raw_body`)**:
  ```json
  {"quote_request_id":"0193132e-4b67-7b2b-8a88-dcf46be1fa3a"}
  ```
- **Cadena a firmar**:
  `1731171600.{"quote_request_id":"0193132e-4b67-7b2b-8a88-dcf46be1fa3a"}`
- **Firma esperada (`X-PP-Signature`)**:
  ```http
  v1=c8afcdae1d2b3a7823d59a5d193c47c6e1cb672703d7f1dc22a96a255faa7f63
  ```

---

### Vector 2: Pedido formalizado (`order.placed`)

- **Secreto compartido (`secret`)**:
  `secret_primary_key_32_bytes_len!`
- **Timestamp (`X-PP-Timestamp`)**:
  `1731171600`
- **Cuerpo HTTP crudo (`raw_body`)**:
  ```json
  {"id":"evt-123","event":"order.placed"}
  ```
- **Cadena a firmar**:
  `1731171600.{"id":"evt-123","event":"order.placed"}`
- **Firma esperada (`X-PP-Signature`)**:
  ```http
  v1=acb110ec4345318f8a5376eb3b61dc6d5bb64d1c37ef183ba4e260b95563c792
  ```

---

## Rotación de Secretos (Zero-Downtime)

Durante los períodos de rotación de credenciales (iniciados desde el portal de farmacia vía `POST /api-credentials/rotate`), PetPassport envía una cabecera con firmas múltiples separadas por coma:

```http
X-PP-Signature: v1=hash_secreto_actual,v1=hash_secreto_anterior
```

El receptor debe verificar la firma contra su secreto activo actual (`webhook_current`). Si falla, y dispone de un secreto previo en ventana de rotación (`webhook_previous`), debe verificarlo contra este último antes de rechazar la petición.

Se recomienda utilizar siempre comparación en tiempo constante (ej. `crypto/subtle.ConstantTimeCompare` en Go o `crypto.timingSafeEqual` en Node.js) para mitigar ataques de temporización.
