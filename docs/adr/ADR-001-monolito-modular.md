# ADR-001: Monolito modular en Go con límites de contexto estrictos

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F1

## Contexto

El sistema tiene al menos siete bounded contexts y un equipo pequeño. Microservicios desde el inicio
multiplican el costo operativo (despliegues, observabilidad distribuida, consistencia entre servicios)
sin que exista aún carga que lo justifique. Al mismo tiempo, un monolito sin límites se convierte en
una bola de barro que impide extraer servicios en F6.

## Decisión

Un único módulo Go (`backend/`) y un único binario con varios *entrypoints* (`cmd/api`, `cmd/worker`,
`cmd/migrate`). Cada bounded context es un paquete bajo `internal/<contexto>/` con estructura hexagonal:

```
internal/petregistry/
  domain/        # agregados, VOs, eventos, invariantes — sin dependencias externas
  app/           # comandos, queries, handlers, puertos (interfaces)
  infra/         # adaptadores: postgres, object storage, otros contextos
  http/          # handlers HTTP, DTOs, mapeo de errores
```

Reglas de dependencia (verificadas en CI con `go-arch-lint` o `depguard`):

1. `domain` no importa nada fuera de `internal/shared/kernel` y la stdlib.
2. `app` importa `domain` y define puertos; no importa `infra` ni `http`.
3. Un contexto **nunca** importa el `domain`/`infra` de otro contexto. Se comunica por:
   - puertos de consulta definidos en el consumidor e implementados en `infra` del consumidor
     llamando al `app` del proveedor (in-process), o
   - suscripción a eventos de dominio publicados por el bus in-process.
4. Ningún contexto lee tablas de otro contexto. Cada contexto posee sus esquemas de BD.
5. `internal/platform` (eventstore, bus, db, auth, http server, telemetry) es infraestructura compartida
   y no contiene reglas de negocio.

## Consecuencias

- Un solo despliegue, una sola transacción de BD por comando: simplicidad en F1–F3.
- La extracción de un contexto a servicio en F6 es mecánica: el puerto in-process pasa a ser un cliente HTTP/gRPC
  y el bus in-process pasa a ser el outbox ya existente.
- Exige disciplina: las reglas de importación se automatizan, no se confían a la revisión.

## Alternativas consideradas

- **Microservicios desde F1**: descartado por costo operativo y por riesgo de dibujar mal los límites antes de conocer el dominio.
- **Monolito por capas (controllers/services/repositories globales)**: descartado porque no protege los límites de contexto.
