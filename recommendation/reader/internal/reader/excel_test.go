package reader

import (
	"os"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/model"
	"github.com/xuri/excelize/v2"
)

func createTmpFile(tmpFile string, data []string) (func(), error) {
	f := excelize.NewFile()
	sheet := "Sheet1"
	header := []string{
		"Environment", "Namespace", "Workload Name", "Type", "POD", "Container", "Replicas",
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

func AssertEqual(t *testing.T, expected []string, actual *model.Recommendation) {
	if actual.Environment != expected[0] {
		t.Errorf("Environment expected %s, got %s", expected[0], actual.Environment)
	}
	if actual.Namespace != expected[1] {
		t.Errorf("Namespace expected %s, got %s", expected[1], actual.Namespace)
	}
	if actual.WorkloadName != expected[2] {
		t.Errorf("WorkloadName expected %s, got %s", expected[2], actual.WorkloadName)
	}
	if actual.Type != model.DeploymentType(expected[3]) {
		t.Errorf("Type expected %s, got %s", expected[3], actual.Type)
	}
	if actual.Pod != expected[4] {
		t.Errorf("Pod expected %s, got %s", expected[4], actual.Pod)
	}
	if actual.Container != expected[5] {
		t.Errorf("Container expected %s, got %s", expected[5], actual.Container)
	}
	if actual.Replicas != expected[6] {
		t.Errorf("Replicas expected %s, got %s", expected[6], actual.Replicas)
	}
	if actual.CpuRequest != NormalizeNumericData(expected[7], "m") {
		t.Errorf("CPU Request expected %s, got %s", expected[7], actual.CpuRequest)
	}
	if actual.CpuLimit != NormalizeNumericData(expected[8], "m") {
		t.Errorf("CPU Limit expected %s, got %s", expected[8], actual.CpuLimit)
	}
	if actual.CpuRequestRecommendation != NormalizeNumericData(expected[9], "m") {
		t.Errorf("CPU Rec expected %s, got %s", expected[9], actual.CpuRequestRecommendation)
	}
	if actual.CpuLimitRecommendation != NormalizeNumericData(expected[10], "m") {
		t.Errorf("CPU Lim Rec expected %s, got %s", expected[10], actual.CpuLimitRecommendation)
	}
	if actual.MemRequest != NormalizeNumericData(expected[11], "Mi") {
		t.Errorf("Mem Request expected %s, got %s", expected[11], actual.MemRequest)
	}
	if actual.MemLimit != NormalizeNumericData(expected[12], "Mi") {
		t.Errorf("Mem Limit expected %s, got %s", expected[12], actual.MemLimit)
	}
	if actual.MemoryRequestRecommendation != NormalizeNumericData(expected[13], "Mi") {
		t.Errorf("Mem Rec expected %s, got %s", expected[13], actual.MemoryRequestRecommendation)
	}
	if actual.MemoryLimitRecommendation != NormalizeNumericData(expected[14], "Mi") {
		t.Errorf("Mem Lim Rec expected %s, got %s", expected[14], actual.MemoryLimitRecommendation)
	}
}

func TestReadFromExcel(t *testing.T) {
	// 1. Setup: create a temporary Excel file with test data
	tmpFile := "test_recommendations.xlsx"
	// data row (1 row of data starting from the second row)
	data := []string{
		"development", "default", "workload-name", "ReplicaSet", "my-pod-abc", "nginx", "3",
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
	AssertEqual(t, data, &r)

}

func TestReadFromExcelNormalizingData(t *testing.T) {
	// 1. Setup: create a temporary Excel file with test data
	tmpFile := "test_recommendations.xlsx"
	// data row (1 row of data starting from the second row)
	data := []string{
		"prod", "default", "", "ReplicaSet", "my-pod-abc", "nginx", "3",
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
	AssertEqual(t, data, &r)
}
