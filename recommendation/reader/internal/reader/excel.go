package reader

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mcpunzo/k8s-rightsizer/model"
	excelize "github.com/xuri/excelize/v2"
)

type ExcelReader struct {
	FilePath string
}

// Read reads the Excel file and returns a slice of Recommendation structs
// It assumes that the first row of the Excel file contains headers and that the data starts from the second row
// The expected columns in the Excel file are: Environment, Namespace, Workload Name, Type (ReplicaSet o StatefulSet), POD, Container, Replicas, CPU Request [mCores], CPU Limit [mCores], CPU Request Recommended [mCores], CPU Limit Recommended [mCores], MEM Request [MB], MEM Limit [MB], MEM Request Recommended [MB], MEM Limit Recommended [MB]
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

		// min lenght check to avoid index out of range, we expect at least 14 columns based on the struct and the excel format
		if len(row) < 15 {
			continue
		}

		rec := model.Recommendation{
			Environment:                 row[0],
			Namespace:                   row[1],
			WorkloadName:                row[2],
			Type:                        model.DeploymentType(row[3]),
			Pod:                         row[4],
			Container:                   row[5],
			Replicas:                    row[6],
			CpuRequest:                  NormalizeNumericData(row[7], "m"),
			CpuLimit:                    NormalizeNumericData(row[8], "m"),
			CpuRequestRecommendation:    NormalizeNumericData(row[9], "m"),
			CpuLimitRecommendation:      NormalizeNumericData(row[10], "m"),
			MemRequest:                  NormalizeNumericData(row[11], "Mi"),
			MemLimit:                    NormalizeNumericData(row[12], "Mi"),
			MemoryRequestRecommendation: NormalizeNumericData(row[13], "Mi"),
			MemoryLimitRecommendation:   NormalizeNumericData(row[14], "Mi"),
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

	// if val is a pure number then add suffix
	if isNumeric := regexp.MustCompile(`^[0-9]+$`).MatchString(val); isNumeric {
		return val + suffix
	}

	return val
}
