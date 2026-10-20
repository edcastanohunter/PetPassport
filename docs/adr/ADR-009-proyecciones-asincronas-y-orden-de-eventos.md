# ADR-009: Proyecciones asíncronas por suscripción catch-up; outbox solo para efectos externos; orden total garantizado

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F2

## Contexto

F1 ejecuta todas las proyecciones en la transacción del comando (ADR-002). F2 añade proyecciones derivadas
y entre contextos (línea de tiempo clínica, próximas vacunas, historial de peso), notificaciones y, en F3,
OpenSearch. Ejecutarlas todas de forma síncrona acopla la latencia del comando al número de consumidores
y hace que un fallo en un consumidor secundario rechace un comando clínico válido.

Dos problemas técnicos hay que resolver bien:

1. **Qué va síncrono y qué asíncrono**, sin romper *read-your-writes* en la UI.
2. **Orden y huecos**: `global_position` es una `IDENTITY`; dos transacciones pueden obtener posiciones 10 y 11
   y confirmar en orden inverso. Un lector que ya vio 11 se saltaría 10 para siempre.

## Decisión

### 1. Híbrido síncrono/asíncrono

| Tipo de proyección | Modo | Ejemplos |
|---|---|---|
| Read model **primario** del propio contexto (lo que el comando devuelve en su respuesta) | Síncrono, misma tx (como F1) | `read_petregistry.pets`, `read_clinical.encounters`, `read_scheduling.appointments` |
| Derivadas, entre contextos, agregadas | Asíncrono (worker) | `medical_timeline`, `vaccination_due`, `weight_history`, `org_stats` |
| Efectos externos | Asíncrono vía **outbox** | emails, webhooks (F4), OpenSearch (F3, por suscripción) |

Todo proyector, síncrono o asíncrono, es idempotente y reconstruible; el modo es configuración, no código.

### 2. Suscripciones catch-up para proyecciones; outbox para integración

- Los proyectores asíncronos leen **directamente `es.events`** por `global_position` con un *checkpoint* por
  proyección (`platform.projection_checkpoints`). No usan el outbox.
- El **outbox** (`es.outbox`) se reserva a *process managers* con efectos externos (Notifications, F4 partners).
  Su consumidor marca `published_at` y lleva reintentos/dead-letter propios.

### 3. Orden total sin huecos

`Append` toma `SELECT pg_advisory_xact_lock(<const es.append>)` **antes** de insertar en `es.events`. El lock
se libera al confirmar, por lo que las posiciones se asignan y se hacen visibles en orden estricto; un lector
nunca ve `N+1` antes que `N`.

- Coste: serializa todos los appends del sistema. Con transacciones de comando de pocos ms esto permite
  cientos de comandos/s, muy por encima de F2–F4. Métrica `eventstore_append_lock_wait_seconds`.
- Revisión en F3: si el *lock wait* p95 supera 20 ms de forma sostenida, se pasa a lectura tolerante a huecos
  (los lectores esperan hasta 500 ms por posiciones faltantes y consultan `pg_snapshot_xip` para distinguir
  "en vuelo" de "nunca existirá").

### 4. Semántica de entrega

- *At-least-once*. Los proyectores son idempotentes por `(projection, event_id)` o por comparación de versión.
- Un proyector que falla reintenta con *backoff* (hasta 5 veces); si persiste, **se detiene esa proyección**
  (*fail-stop*) y alerta. Nunca se salta un evento: un read model desactualizado es preferible a uno incorrecto.
- Los *process managers* (notificaciones) sí tienen *dead-letter*: un email que no se puede enviar no debe
  bloquear el resto.

## Consecuencias

- La UI puede mostrar "actualizando" en vistas derivadas; las vistas primarias son consistentes de inmediato.
- `cmd/worker` pasa a ser un proceso obligatorio en F2 (un solo replica; particionado en F3).
- Rebuild de una proyección = reset de checkpoint + truncado de sus tablas + replay; sin tocar `es.events`.

## Alternativas consideradas

- **Todo asíncrono + "esperar a la proyección" en la API**: elegante, pero añade latencia y complejidad en cada
  respuesta; se reconsidera si el híbrido genera inconsistencias en la práctica.
- **Outbox como única fuente para proyecciones**: duplica el log y obliga a mantener el outbox para siempre.
- **Tolerar huecos desde F2**: correcto pero más difícil de probar; el advisory lock es trivial de verificar.
