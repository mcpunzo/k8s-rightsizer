package model

type DeploymentType string

const (
	ReplicaSet DeploymentType = "ReplicaSet"
	StatefuSet DeploymentType = "StatefuSet"
)

type Recommendation struct {
	Namespace                   string         `json:"Namespace"`
	Pod                         string         `json:"Pod"`
	Type                        DeploymentType `json:"Type"`
	Container                   string         `json:"Container"`
	CpuRequest                  string         `json:"CpuRequest"`
	CpuLimit                    string         `json:"CpuLimit"`
	CpuRequestRecommendation    string         `json:"CpuRequestRecommendation"`
	MemRequest                  string         `json:"MemRequest"`
	MemLimit                    string         `json:"MemLimit"`
	MemoryRequestRecommendation string         `json:"MemoryRequestRecommendation"`
}
