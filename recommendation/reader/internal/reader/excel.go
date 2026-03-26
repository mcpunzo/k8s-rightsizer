package reader

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mcpunzo/k8s-rightsizer/recommendation/model"
	excelize "github.com/xuri/excelize/v2"
)

type ExcelReader struct {
	FilePath string
}

// Read reads the Excel file and returns a slice of Recommendation structs
// It assumes that the first row of the Excel file contains headers and that the data starts from the second row
// The expected columns in the Excel file are: L'excel contiene le colonne: Namespace, Type (ReplicaSet o StatefulSet), POD, Container, Replicas, CPU Request [mCores], CPU Limit [mCores], CPU Request Recommended [mCores], CPU Limit Recommended [mCores], MEM Request [MB], MEM Limit [MB], MEM Request Recommended [MB], MEM Limit Recommended [MB]
// returns an error if there is an issue opening the file or reading the rows
func (r *ExcelReader) Read() ([]model.Recommendation, error) {
	f, err := excelize.OpenFile(r.FilePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %w", err)
	}
	defer f.Close()

	// let's assume the data is in the first sheet
	sheetName := f.GetSheetName(0)
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("error reading rows: %w", err)
	}

	var recommendations []model.Recommendation

	// Skipping the header row (i=0) and starting from the first data row (i=1)
	for i := 1; i < len(rows); i++ {
		row := rows[i]

		// min lenght check to avoid index out of range, we expect at least 13 columns based on the struct and the excel format
		if len(row) < 13 {
			continue
		}

		rec := model.Recommendation{
			Namespace:                   row[0],
			Type:                        model.DeploymentType(row[1]),
			Pod:                         row[2],
			Container:                   row[3],
			CpuRequest:                  NormalizeNumericData(row[5], "m"),
			CpuLimit:                    NormalizeNumericData(row[6], "m"),
			CpuRequestRecommendation:    NormalizeNumericData(row[7], "m"),
			MemRequest:                  NormalizeNumericData(row[9], "Mi"),
			MemLimit:                    NormalizeNumericData(row[10], "Mi"),
			MemoryRequestRecommendation: NormalizeNumericData(row[11], "Mi"),
		}

		recommendations = append(recommendations, rec)
	}

	return recommendations, nil
}

func NormalizeNumericData(val string, suffix string) string {
	val = strings.TrimSpace(val)
	if val == "" {
		return ""
	}

	// if Se è un numero puro (es. "100"), aggiungiamo "m"
	if isNumeric := regexp.MustCompile(`^[0-9]+$`).MatchString(val); isNumeric {
		return val + suffix
	}

	return val
}
