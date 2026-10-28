// Package notifications es el subdominio genérico de notificaciones
// (PP-F2-DOM-002, ADR-012): no es event-sourced; un process manager consume
// el outbox (platform/process) y escribe notifications.scheduled y
// notifications.deliveries. Clinical y Scheduling no saben nada de emails:
// solo emiten eventos.
//
// El módulo se implementa en M2.4; M2.0 entrega el consumidor del outbox,
// el framework de process managers y el scheduler de comandos.
package notifications
