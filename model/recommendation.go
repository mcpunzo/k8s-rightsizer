package model

import "fmt"

type Kind string

const (
	ReplicaSet  Kind = "ReplicaSet"
	StatefulSet Kind = "StatefuSet"
)

type Recommendation struct {
	Environment                 string `json:"Environment"`
	Namespace                   string `json:"Namespace"`
	Kind                        Kind   `json:"Kind"`
	WorkloadName                string `json:"WorkloadName"`
	Container                   string `json:"Container"`
	Replicas                    string `json:"Replicas"`
	CpuRequest                  string `json:"CpuRequest"`
	CpuLimit                    string `json:"CpuLimit"`
	CpuRequestRecommendation    string `json:"CpuRequestRecommendation"`
	CpuLimitRecommendation      string `json:"CpuLimitRecommendation"`
	MemRequest                  string `json:"MemRequest"`
	MemLimit                    string `json:"MemLimit"`
	MemoryRequestRecommendation string `json:"MemoryRequestRecommendation"`
	MemoryLimitRecommendation   string `json:"MemoryLimitRecommendation"`
}

func (r *Recommendation) String() string {
	return fmt.Sprintf(
		"Environment=%s Namespace=%s WorkloadName=%s Kind=%s Container=%s Replicas=%s CpuRequest=%s CpuLimit=%s CpuRequestRecommendation=%s CpuLimitRecommendation=%s MemRequest=%s MemLimit=%s MemoryRequestRecommendation=%s MemoryLimitRecommendation=%s",
		r.Environment,
		r.Namespace,
		r.WorkloadName,
		r.Kind,
		r.Container,
		r.Replicas,
		r.CpuRequest,
		r.CpuLimit,
		r.CpuRequestRecommendation,
		r.CpuLimitRecommendation,
		r.MemRequest,
		r.MemLimit,
		r.MemoryRequestRecommendation,
		r.MemoryLimitRecommendation,
	)
}

// DeepCopy creates a deep copy of the Recommendation struct.
// This is useful for scenarios where you want to modify a copy of the recommendation without affecting the original.
// returns: A pointer to a new Recommendation struct that is a deep copy of the original.
func (r *Recommendation) DeepCopy() *Recommendation {
	if r == nil {
		return nil
	}

	newRec := *r
	return &newRec
}
