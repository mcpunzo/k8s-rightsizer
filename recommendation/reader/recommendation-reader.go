package reader

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mcpunzo/k8s-rightsizer/model"
	ir "github.com/mcpunzo/k8s-rightsizer/recommendation/reader/internal/reader"
)

type RecommendationsReader interface {
	Read() ([]model.Recommendation, error)
}

func NewReader(filePath string) (RecommendationsReader, error) {
	ext := GetFileExtension(filePath)
	switch ext {
	case "xlsx", "xls":
		return &ir.ExcelReader{FilePath: filePath}, nil
	// Add more cases for other file formats (e.g., CSV, JSON) as needed
	default:
		return nil, fmt.Errorf("unsupported file format: %s", filePath)
	}
}

func GetFileExtension(filePath string) string {
	ext := filepath.Ext(filePath)
	return strings.ToLower(strings.TrimPrefix(ext, "."))
}
