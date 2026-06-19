package model

import (
	"strings"
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

func TestRecommendation_String(t *testing.T) {
	t.Parallel()
	rec := &Recommendation{
		Environment:                 "production",
		Namespace:                   "billing",
		Kind:                        Deployment,
		WorkloadName:                "api-server",
		Container:                   "app",
		Replicas:                    "3",
		CpuRequest:                  "100m",
		CpuLimit:                    "500m",
		CpuRequestRecommendation:    "200m",
		CpuLimitRecommendation:      "400m",
		MemRequest:                  "128Mi",
		MemLimit:                    "512Mi",
		MemoryRequestRecommendation: "256Mi",
		MemoryLimitRecommendation:   "1Gi",
	}

	got := rec.String()

	// Verify key fields are present in the output
	expectedParts := []string{
		"Environment=production",
		"Namespace=billing",
		"WorkloadName=api-server",
		"Kind=Deployment",
		"Container=app",
		"CpuRequestRecommendation=200m",
		"MemoryRequestRecommendation=256Mi",
	}

	for _, part := range expectedParts {
		if !strings.Contains(got, part) {
			t.Errorf("String() missing %q in output: %s", part, got)
		}
	}
}
