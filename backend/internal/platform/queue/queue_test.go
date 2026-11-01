package queue

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestQueuePriorities(t *testing.T) {
	if QueuePriorities[QueueCritical] != 6 {
		t.Errorf("esperada prioridad 6 para critical, obtenido %d", QueuePriorities[QueueCritical])
	}
	if QueuePriorities[QueueDefault] != 3 {
		t.Errorf("esperada prioridad 3 para default, obtenido %d", QueuePriorities[QueueDefault])
	}
	if QueuePriorities[QueueLow] != 1 {
		t.Errorf("esperada prioridad 1 para low, obtenido %d", QueuePriorities[QueueLow])
	}
}

func TestPayloadSerialization(t *testing.T) {
	id := uuid.New()
	emailPayload := NotifyEmailPayload{DeliveryID: id}
	data, err := json.Marshal(emailPayload)
	if err != nil {
		t.Fatalf("marshal email payload: %v", err)
	}

	var parsed NotifyEmailPayload
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal email payload: %v", err)
	}
	if parsed.DeliveryID != id {
		t.Errorf("esperado deliveryID %s, obtenido %s", id, parsed.DeliveryID)
	}

	docPayload := DocRenderPayload{Kind: "encounter", Ref: id.String(), Version: 2}
	data, err = json.Marshal(docPayload)
	if err != nil {
		t.Fatalf("marshal doc payload: %v", err)
	}
	var parsedDoc DocRenderPayload
	if err := json.Unmarshal(data, &parsedDoc); err != nil {
		t.Fatalf("unmarshal doc payload: %v", err)
	}
	if parsedDoc.Kind != "encounter" || parsedDoc.Version != 2 {
		t.Errorf("doc payload parseado incorrecto: %+v", parsedDoc)
	}
}
