package domain

import "strings"

// DedupKey es la clave natural de idempotencia (NOT-006):
// regla|sujeto|destinatario|ocurrencia. La ocurrencia es el event_id en las
// inmediatas y el desplazamiento (offset) en las programadas. Reprocesar el
// outbox produce la misma clave y, por la restricción UNIQUE, ninguna fila nueva.
func DedupKey(rule, subject, recipient, occurrence string) string {
	return strings.Join([]string{rule, subject, recipient, occurrence}, "|")
}
