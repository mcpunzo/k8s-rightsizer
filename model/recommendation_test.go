package model

import (
	"testing"
)

func TestRecommendation_DeepCopy(t *testing.T) {
	original := &Recommendation{
		Environment:                 "production",
		Namespace:                   "billing",
		Type:                        "Deployment", // Assumendo sia una stringa o tipo derivato
		WorkloadName:                "api-server",
		Container:                   "app-container",
		CpuRequestRecommendation:    "500m",
		MemoryRequestRecommendation: "1Gi",
	}

	copy := original.DeepCopy()

	if original == copy {
		t.Fatal("DeepCopy ha restituito lo stesso puntatore dell'originale")
	}

	if copy.WorkloadName != original.WorkloadName {
		t.Errorf("Valore non copiato correttamente: got %s, want %s", copy.WorkloadName, original.WorkloadName)
	}

	if copy.CpuRequestRecommendation != original.CpuRequestRecommendation {
		t.Errorf("Valore non copiato correttamente: got %s, want %s", copy.CpuRequestRecommendation, original.CpuRequestRecommendation)
	}

	copy.WorkloadName = "modified-name"
	if original.WorkloadName == "modified-name" {
		t.Error("La modifica alla copia ha influenzato l'originale (shallow copy rilevata)")
	}

	var nilRec *Recommendation
	if nilRec.DeepCopy() != nil {
		t.Error("DeepCopy su un puntatore nil dovrebbe restituire nil")
	}
}
