# ADR-015: Escalado horizontal — `api` sin estado con N réplicas; `worker` con arrendamiento (lease) por proyección; Helm en F3, migración a Kubernetes en F5

- Estado: Aceptada (actualiza ADR-008)
- Fecha: 2026-10-20
- Fase: F3

## Contexto

ADR-008 fijó VM + Compose y pospuso Kubernetes "cuando el worker necesite escalar horizontalmente (F3)".
F3 llega con objetivos de rendimiento y con dos procesos que deben poder replicarse. Hay que decidir si
F3 migra a Kubernetes o si Compose aguanta una fase más.

## Decisión

1. **`api`**: ya es sin estado salvo la caché en memoria de membresías, que pasa a Redis (ADR-014). Se ejecuta
   con N réplicas (2 en staging, 2–3 en prod) detrás de Traefik (round-robin, health checks). El advisory
   lock de `Append` y las restricciones de exclusión son de base de datos, por lo que funcionan entre réplicas.
2. **`worker`**: N réplicas. Cada proyección/runner se asigna a **una** réplica mediante *leases* en
   `platform.worker_leases` (`name, holder, expires_at`, renovación cada 10 s, TTL 30 s). Una réplica que
   cae libera sus leases por expiración; otra los toma. Esto da alta disponibilidad y reparto de carga a nivel
   de proyección. El particionado **dentro** de una proyección (por `hash(stream_id)`) se diseña pero solo se
   activa si una proyección individual no sigue el ritmo (`projection_lag` sostenido), y se registra como
   COULD.
3. **Plataforma**: F3 mantiene Compose en staging/prod con `--scale api=2 --scale worker=2` y añade un
   **Helm chart** (`infra/helm/petpassport`) validado en un clúster efímero (kind) en CI. La migración de
   producción a un Kubernetes gestionado se ejecuta en F5 (junto con DR y multi-zona), salvo que la carga
   del piloto supere la capacidad de la VM antes (señal: CPU > 70% sostenida o p95 fuera de SLO con 3 réplicas).
4. Datos gestionados ya desde F1; F3 añade Redis y OpenSearch gestionados en producción (contenedores en dev;
   en staging, contenedores salvo que el coste lo permita).

## Consecuencias

- Ningún componente depende de estar solo: todo lock o singleton es explícito (lease o lock de BD).
- Los despliegues pasan a ser *rolling* (réplica a réplica) con `readyz`; Traefik deja de enviar tráfico
  a la réplica que se apaga.
- El chart de Helm es la especificación de despliegue "oficial" desde F3 aunque prod siga en Compose:
  ambos se generan de los mismos valores (`infra/values/*.yaml`) para no mantener dos verdades.

## Alternativas consideradas

- Migrar a Kubernetes en F3: pospone 3–4 semanas la funcionalidad de búsqueda sin necesidad demostrada.
- Un worker único "grande": sin HA; descartado.
