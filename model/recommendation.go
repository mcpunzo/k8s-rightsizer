package model

import "fmt"

type DeploymentType string

const (
	ReplicaSet DeploymentType = "ReplicaSet"
	StatefuSet DeploymentType = "StatefuSet"
)

type Recommendation struct {
	Environment                 string         `json:"Environment"`
	Namespace                   string         `json:"Namespace"`
	WorkloadName                string         `json:"WorkloadName,omitempty"`
	Pod                         string         `json:"Pod"`
	Type                        DeploymentType `json:"Type"`
	Container                   string         `json:"Container"`
	Replicas                    string         `json:"Replicas"`
	CpuRequest                  string         `json:"CpuRequest"`
	CpuLimit                    string         `json:"CpuLimit"`
	CpuRequestRecommendation    string         `json:"CpuRequestRecommendation"`
	CpuLimitRecommendation      string         `json:"CpuLimitRecommendation"`
	MemRequest                  string         `json:"MemRequest"`
	MemLimit                    string         `json:"MemLimit"`
	MemoryRequestRecommendation string         `json:"MemoryRequestRecommendation"`
	MemoryLimitRecommendation   string         `json:"MemoryLimitRecommendation"`
}

func (r *Recommendation) String() string {
	return fmt.Sprintf(
		"Environment=%s Namespace=%s WorkloadName=%s Pod=%s Type=%s Container=%s Replicas=%s CpuRequest=%s CpuLimit=%s CpuRequestRecommendation=%s CpuLimitRecommendation=%s MemRequest=%s MemLimit=%s MemoryRequestRecommendation=%s MemoryLimitRecommendation=%s",
		r.Environment,
		r.Namespace,
		r.WorkloadName,
		r.Pod,
		r.Type,
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
