package model

import (
	"testing"
)

func TestRecommendation_WorkloadID(t *testing.T) {
	rec := &Recommendation{
		Environment:  "production",
		Namespace:    "billing",
		Kind:         "Deployment",
		WorkloadName: "api-server",
	}

	got := rec.WorkloadID()
	want := "production-billing-Deployment-api-server"

	if got != want {
		t.Fatalf("WorkloadID() = %q, want %q", got, want)
	}
}

func TestRecommendation_ContainerID(t *testing.T) {
	rec := &Recommendation{
		Environment:  "production",
		Namespace:    "billing",
		Kind:         "Deployment",
		WorkloadName: "api-server",
		Container:    "app-container",
	}

	got := rec.ContainerID()
	want := "production-billing-Deployment-api-server-app-container"

	if got != want {
		t.Fatalf("ContainerID() = %q, want %q", got, want)
	}
}

func TestRecommendation_DeepCopy(t *testing.T) {
	original := &Recommendation{
		Environment:                 "production",
		Namespace:                   "billing",
		Kind:                        "Deployment",
		WorkloadName:                "api-server",
		Container:                   "app-container",
		CpuRequestRecommendation:    "500m",
		MemoryRequestRecommendation: "1Gi",
	}

	copy := original.DeepCopy()

	if original == copy {
		t.Fatal("DeepCopy returned the same pointer as the original")
	}

	if copy.WorkloadName != original.WorkloadName {
		t.Errorf("Value not copied correctly: got %s, want %s", copy.WorkloadName, original.WorkloadName)
	}

	if copy.CpuRequestRecommendation != original.CpuRequestRecommendation {
		t.Errorf("Value not copied correctly: got %s, want %s", copy.CpuRequestRecommendation, original.CpuRequestRecommendation)
	}

	copy.WorkloadName = "modified-name"
	if original.WorkloadName == "modified-name" {
		t.Error("Modifying the copy affected the original (shallow copy detected)")
	}

	var nilRec *Recommendation
	if nilRec.DeepCopy() != nil {
		t.Error("DeepCopy on a nil pointer should return nil")
	}
}
