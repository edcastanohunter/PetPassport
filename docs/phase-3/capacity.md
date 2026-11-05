# Plan de Capacidad y Reglas de Escalado (PP-F3-PRF-008)

> Estado: Aprobado · Fecha: 2026-11-05 · Ámbito: F3 M3.3 Rendimiento

Este documento establece el dimensionamiento de infraestructura, consumos nominales medidos en staging y reglas operativas de escalado horizontal para PetPassport.

---

## 1. Consumo de Recursos a Carga Nominal (100 req/s, 20 comandos/s)

Configuración base: 2 réplicas `api`, 2 réplicas `worker` en VM de staging (16 GB RAM, 4 vCPU):

| Componente | Réplicas | CPU Promedio / Pico | RAM Promedio / Pico | Conexiones PG |
|---|---|---|---|---|
| `api` | 2 | 22% / 45% (por réplica) | 180 MB / 290 MB | 20 (pool máx por réplica = 40 total) |
| `worker` | 2 | 15% / 38% (por réplica) | 210 MB / 340 MB | 10 (pool máx por réplica = 20 total) |
| PostgreSQL 17 | 1 | 25% / 60% | 4.2 GB (shared_buffers 4 GB) | 65 pico (de 100 máx) |
| Redis 7 | 1 | 4% / 12% | 145 MB (memoria total en uso) | N/A |
| OpenSearch 2.18 | 1-2 | 30% / 65% | 3.8 GB (Heap 2 GB) | N/A |
| Traefik v3 | 1 | 8% / 18% | 85 MB | N/A |

---

## 2. Dimensionamiento por Dimensión de Datos

### 2.1. Memoria Redis (DB 0 Caché y Rate Limit, DB 1 Asynq Jobs)
- **DB 0 (Caché y Ventanas Deslizantes de Rate Limit)**:
  - Huella de membresías y permisos: ~1.2 KB por usuario activo.
  - Rate limiting sliding windows: ~0.4 KB por par (IP/Usuario, ruta).
  - Resúmenes de mascotas y organizaciones: ~2.5 KB por clave.
  - Consumo estimado a escala (80k mascotas, 50k tutores): **120 MB – 220 MB**.
  - Política de desalojo: `maxmemory 512mb` con política `allkeys-lru`. Alerta a 80% (410 MB).
- **DB 1 (Asynq Jobs)**:
  - Cola `default`, `critical`, `low`: ~25 MB con backlog normal (< 1.000 tareas en tránsito).
  - Persistencia: AOF `everysec`.

### 2.2. Tamaño de Índices OpenSearch (por cada 10.000 mascotas)
- **Índice `pets`**:
  - ~10.000 documentos con subcampos de análisis n-gram e ICU folding.
  - Tamaño en disco: **~18 MB** (con 1 réplica = 36 MB).
- **Índice `clinical`**:
  - Ratio de ~5 consultas firmadas por mascota (~50.000 encuentros).
  - Texto diagnóstico, tratamientos, notas y términos vectorizados.
  - Tamaño en disco: **~85 MB** (con 1 réplica = 170 MB).
- **Índice `medications`**:
  - Catálogo base (~12.000 medicamentos): **~22 MB**.
- **Total por 10k mascotas**: **~125 MB** (250 MB con réplica de alta disponibilidad).

### 2.3. Crecimiento Mensual Estimado de `es.events`
- Eventos por mascota/año: ~18 eventos (altas, vacunas, consultas, citas, recordatorios).
- Para una base activa de 80.000 mascotas en fase piloto:
  - Nuevos eventos/mes: **~120.000 eventos/mes**.
  - Tamaño de payload promedio: 450 bytes + 200 bytes metadata/índices = ~650 bytes/evento.
  - Crecimiento mensual en PostgreSQL: **~78 MB/mes** (~950 MB/año).

---

## 3. Punto de Saturación Medido (`spike` test)

En las pruebas de estrés con factor x5 (500 req/s, 100 comandos/s):

- **Punto de degradación del SLO**:
  - A **380 req/s sostenidos** con la configuración nominal (2 réplicas `api`), la latencia p95 de `/search/clinical` alcanza los **285 ms** (cerca del límite de 300 ms).
  - A **460 req/s sostenidos**, la CPU de las réplicas `api` supera el 85% y la latencia p95 supera los **350 ms**, rompiendo el SLO.
- **Punto de recuperación**:
  - Finalizado el spike, el sistema restablece la latencia nominal (< 80 ms) en **menos de 15 segundos**, sin necesidad de reinicio de procesos ni pérdida de tareas en colas.

---

## 4. Reglas de Escalado Horizontal (HPA / Kubernetes / VMs)

Las reglas operativas automáticas son las siguientes:

1. **Escalado de Réplicas `api`**:
   - **Métrica**: CPU promedio de las réplicas `api` > **70%** durante **10 minutos** consecutivos.
   - **Acción**: Incrementar réplicas `api` en +1 (hasta un máximo de 5 réplicas).
   - **Límite de conexiones PG**: Cada réplica suma 20 conexiones. Si las réplicas superan 3 (60 conns + 20 worker = 80 conns), se activa la regla de despliegue de **PgBouncer** (PRF-005).

2. **Escalado de Réplicas `worker`**:
   - **Métrica**: `projection_lag_seconds` p95 > **1.0 s** durante **10 minutos** consecutivos en cualquier proyección, O backlog de cola Asynq `default` > 5.000 tareas por 5 min.
   - **Acción**: Incrementar réplicas `worker` en +1 (hasta un máximo de 4 réplicas). Los leases de worker (`platform.worker_leases`) se redistribuyen automáticamente con jitter en menos de 30 s.
