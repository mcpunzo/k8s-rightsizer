package reader

import (
	"fmt"
	"strings"

	"github.com/mcpunzo/k8s-rightsizer/model"
	excelize "github.com/xuri/excelize/v2"
)

type ExcelReader struct {
	FilePath string
}

// Read reads the Excel file and returns a slice of Recommendation structs
// It assumes that the first row of the Excel file contains headers and that the data starts from the second row
// The expected columns in the Excel file are: Environment, Namespace, Kind (Deployment o StatefulSet), Workload Name, Container, Replicas, CPU Request [mCores], CPU Limit [mCores], CPU Request Recommended [mCores], CPU Limit Recommended [mCores], MEM Request [MB], MEM Limit [MB], MEM Request Recommended [MB], MEM Limit Recommended [MB]
// returns an error if there is an issue opening the file or reading the rows
func (r *ExcelReader) Read() ([]model.Recommendation, error) {
	f, err := excelize.OpenFile(r.FilePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	// let's assume the data is in the first sheet
	sheetName := f.GetSheetName(0)
	rows, err := f.Rows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("error reading rows: %w", err)
	}
	defer rows.Close()

	var recommendations []model.Recommendation
	rowIndex := 0

	// Skip the first row (headers) and parse data rows as a stream.
	for rows.Next() {
		rowIndex++
		if rowIndex == 1 {
			continue
		}

		row, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("error reading row %d: %w", rowIndex, err)
		}

		// min lenght check to avoid index out of range, we expect at least 14 columns based on the struct and the excel format
		if len(row) < 14 {
			continue
		}

		rec := model.Recommendation{
			Environment:                 row[0],
			Namespace:                   row[1],
			Kind:                        model.Kind(row[2]),
			WorkloadName:                row[3],
			Container:                   row[4],
			Replicas:                    row[5],
			CpuRequest:                  NormalizeNumericData(row[6], "m"),
			CpuLimit:                    NormalizeNumericData(row[7], "m"),
			CpuRequestRecommendation:    NormalizeNumericData(row[8], "m"),
			CpuLimitRecommendation:      NormalizeNumericData(row[9], "m"),
			MemRequest:                  NormalizeNumericData(row[10], "Mi"),
			MemLimit:                    NormalizeNumericData(row[11], "Mi"),
			MemoryRequestRecommendation: NormalizeNumericData(row[12], "Mi"),
			MemoryLimitRecommendation:   NormalizeNumericData(row[13], "Mi"),
		}

		recommendations = append(recommendations, rec)
	}

	if err := rows.Error(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return recommendations, nil
}

// NormalizeNumericData normalizes numeric data by adding a suffix if the value is a pure number.
// It trims whitespace and checks if the value is empty or a pure number. If it's a pure number, it appends the specified suffix.
// If the value is not a pure number, it returns the original value.
// param val: The string value to normalize.
// param suffix: The suffix to append if the value is a pure number (e.g., "m" for milli, "Mi" for mebibytes).
// returns: The normalized string value with the suffix added if applicable.
func NormalizeNumericData(val string, suffix string) string {
	val = strings.TrimSpace(val)
	if val == "" {
		return ""
	}

	// if val is a pure number then add suffix
	if isAllDigits(val) {
		return val + suffix
	}

	return val
}

// isAllDigits checks if a string consists entirely of digits (0-9).
// It returns true if the string is non-empty and contains only digit characters, and false otherwise.
// param s: The string to check.
// returns: A boolean indicating whether the string consists entirely of digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}

	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return true
}
