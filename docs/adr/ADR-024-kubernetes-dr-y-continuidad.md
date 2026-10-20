# ADR-024: Migración de producción a Kubernetes gestionado multi-zona; objetivos de continuidad (RPO 5 min, RTO 1 h); backups cross-region y simulacros trimestrales

- Estado: Aceptada (ejecuta ADR-015)
- Fecha: 2026-10-20
- Fase: F5

## Contexto

Con clientes de pago y SLA contractual, una VM única por entorno (ADR-008) ya no es aceptable. El Helm chart
existe desde F3 y los procesos son sin estado o con leases.

## Decisión

1. **Plataforma**: Kubernetes gestionado del proveedor cloud, un clúster por entorno (staging, prod), nodos
   en ≥ 2 zonas; `api` ≥ 3 réplicas con *PodDisruptionBudget* y HPA; `worker` ≥ 2; Traefik como ingress
   (misma configuración de F1–F4 traducida a CRDs); secretos vía External Secrets desde el secret manager;
   imágenes firmadas (cosign) verificadas en admisión.
2. **Datos**: PostgreSQL gestionado multi-AZ con réplica síncrona y *failover* automático; PITR; copia de
   backups a una segunda región (cifrada) con retención 30 días; Redis gestionado con réplica; OpenSearch
   gestionado multi-AZ; object storage con versionado y replicación cross-region.
3. **Objetivos**: RPO ≤ 5 min (PITR + WAL continuo), RTO ≤ 1 h para fallo de zona (automático) y ≤ 4 h para
   pérdida de región (procedimiento manual documentado y ensayado). Disponibilidad objetivo 99,9 % mensual
   (SLA de planes Pro/Enterprise).
4. **Simulacros**: restauración completa en entorno aislado **trimestral** (medición de RPO/RTO real, registro
   en `infra/deploy/RESTORE-LOG.md`); game day semestral con pérdida de zona; prueba anual de pérdida de región
   (restauración desde la copia cross-region + `rebuild --all`).
5. **Migración**: *blue/green* entre la VM y el clúster: réplica lógica de PostgreSQL → ventana de corte
   (< 15 min, en horario de baja actividad, aviso a clientes), DNS en el WAF, rollback posible 24 h.
6. Compose queda solo para desarrollo; los valores compartidos (`infra/values`) siguen siendo la fuente.

## Consecuencias

- Coste mensual de infraestructura sube (estimación en 07-operaciones); justificado por SLA y por ingresos.
- El equipo necesita competencia operativa en Kubernetes (formación o soporte gestionado).

## Alternativas consideradas

- Varias VMs con Compose y balanceo: sin orquestación de fallos ni despliegues sin corte; descartado.
- Plataforma serverless/containers gestionados (Cloud Run/ECS Fargate): válida; se descarta por el worker con
  leases y por mantener una única abstracción (Helm) ya construida.
