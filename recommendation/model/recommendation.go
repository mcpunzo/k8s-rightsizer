package model

type Recommendation struct {
	Namespace                   string `json:"Namespace"`
	Pod                         string `json:"Pod"`
	Container                   string `json:"Container"`
	CpuRequest                  string `json:"CpuRequest"`
	CpuLimit                    string `json:"CpuLimit"`
	CpuRequestRecommendation    string `json:"CpuRequestRecommendation"`
	MemRequest                  string `json:"MemRequest"`
	MemLimit                    string `json:"MemLimit"`
	MemoryRequestRecommendation string `json:"MemoryRequestRecommendation"`
}
