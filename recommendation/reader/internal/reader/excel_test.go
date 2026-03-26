package reader

import (
	"os"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/recommendation/model"
	"github.com/xuri/excelize/v2"
)

func createTmpFile(tmpFile string, data []string) (func(), error) {
	f := excelize.NewFile()
	sheet := "Sheet1"
	// headers (13 columns as expected by the Read function)
	header := []string{
		"Namespace", "Type", "POD", "Container", "Replicas",
		"CPU Req", "CPU Lim", "CPU Req Rec", "CPU Lim Rec",
		"MEM Req", "MEM Lim", "MEM Req Rec", "MEM Lim Rec",
	}
	for i, val := range header {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, val)
	}

	for i, val := range data {
		cell, _ := excelize.CoordinatesToCellName(i+1, 2)
		f.SetCellValue(sheet, cell, val)
	}

	if err := f.SaveAs(tmpFile); err != nil {
		return nil, err
	}

	return func() {
		f.Close()
		os.Remove(tmpFile) // clean up the temporary file after the test
	}, nil
}

func TestReadFromExcel(t *testing.T) {
	// 1. Setup: create a temporary Excel file with test data
	tmpFile := "test_recommendations.xlsx"
	// data row (1 row of data starting from the second row)
	data := []string{
		"default", "ReplicaSet", "my-pod-abc", "nginx", "3",
		"100m", "200m", "150m", "250m",
		"128Mi", "256Mi", "200Mi", "300Mi",
	}
	cleanup, err := createTmpFile(tmpFile, data)
	if err != nil {
		t.Fatalf("Error creating file: %v", err)
	}

	defer cleanup() // ensure cleanup is called after the test

	// 2. test execution
	reader := ExcelReader{FilePath: tmpFile}
	recs, err := reader.Read()
	if err != nil {
		t.Fatalf("Read() returned unexpected error: %v", err)
	}

	// 3. Assertions
	if len(recs) != 1 {
		t.Errorf("Expected 1 record, got %d", len(recs))
	}

	r := recs[0]
	if r.Namespace != "default" {
		t.Errorf("Namespace expected 'default', got '%s'", r.Namespace)
	}
	if r.Type != model.ReplicaSet {
		t.Errorf("Type expected 'ReplicaSet', got '%s'", r.Type)
	}
	if r.CpuRequestRecommendation != "150m" {
		t.Errorf("CPU Rec expected '150m', got '%s'", r.CpuRequestRecommendation)
	}
	if r.MemoryRequestRecommendation != "200Mi" {
		t.Errorf("Mem Rec expected '200Mi', got '%s'", r.MemoryRequestRecommendation)
	}
}

func TestReadFromExcelNormalizingData(t *testing.T) {
	// 1. Setup: create a temporary Excel file with test data
	tmpFile := "test_recommendations.xlsx"
	// data row (1 row of data starting from the second row)
	data := []string{
		"default", "ReplicaSet", "my-pod-abc", "nginx", "3",
		"100", "200", "150", "250",
		"128", "256", "200", "300",
	}
	cleanup, err := createTmpFile(tmpFile, data)
	if err != nil {
		t.Fatalf("Error creating file: %v", err)
	}

	defer cleanup() // ensure cleanup is called after the test

	// 2. test execution
	reader := ExcelReader{FilePath: tmpFile}
	recs, err := reader.Read()
	if err != nil {
		t.Fatalf("Read() returned unexpected error: %v", err)
	}

	// 3. Assertions
	if len(recs) != 1 {
		t.Errorf("Expected 1 record, got %d", len(recs))
	}

	r := recs[0]
	if r.Namespace != "default" {
		t.Errorf("Namespace expected 'default', got '%s'", r.Namespace)
	}
	if r.Type != model.ReplicaSet {
		t.Errorf("Type expected 'ReplicaSet', got '%s'", r.Type)
	}
	if r.CpuRequestRecommendation != "150m" {
		t.Errorf("CPU Rec expected '150m', got '%s'", r.CpuRequestRecommendation)
	}
	if r.MemoryRequestRecommendation != "200Mi" {
		t.Errorf("Mem Rec expected '200Mi', got '%s'", r.MemoryRequestRecommendation)
	}
}
