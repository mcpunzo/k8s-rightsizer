package reader

import (
	"testing"
)

func TestGetFileExtension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filePath string
		want     string
	}{
		{name: "xlsx extension", filePath: "/data/recommendations.xlsx", want: "xlsx"},
		{name: "xls extension", filePath: "/data/recommendations.xls", want: "xls"},
		{name: "csv extension", filePath: "/data/file.csv", want: "csv"},
		{name: "uppercase extension", filePath: "/data/FILE.XLSX", want: "xlsx"},
		{name: "no extension", filePath: "/data/noextension", want: ""},
		{name: "dot in path", filePath: "/data/my.folder/file.xlsx", want: "xlsx"},
		{name: "multiple dots", filePath: "/data/file.backup.xlsx", want: "xlsx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetFileExtension(tt.filePath)
			if got != tt.want {
				t.Errorf("GetFileExtension(%q) = %q, want %q", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestNewReader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		file    string
		wantErr bool
	}{
		{name: "xlsx file returns reader", file: "/tmp/recs.xlsx", wantErr: false},
		{name: "xls file returns reader", file: "/tmp/recs.xls", wantErr: false},
		{name: "unsupported format returns error", file: "/tmp/recs.json", wantErr: true},
		{name: "no extension returns error", file: "/tmp/recs", wantErr: true},
		{name: "csv not supported returns error", file: "/tmp/recs.csv", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, err := NewReader(tt.file)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewReader(%q) error = %v, wantErr %v", tt.file, err, tt.wantErr)
				return
			}
			if !tt.wantErr && reader == nil {
				t.Errorf("NewReader(%q) returned nil reader", tt.file)
			}
		})
	}
}
