package reader

import (
	"github.com/mcpunzo/k8s-rightsizer/recommendation/model"
)

type ExcelReader struct {
	FilePath string
}

func (r *ExcelReader) Read() ([]model.Recommendation, error) {
	/* TODO: Implement the logic to read from Excel file and return recommendations */
	return nil, nil
}
