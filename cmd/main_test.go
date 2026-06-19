package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSetLogLevel(t *testing.T) {
	t.Parallel()

	levels := []string{"debug", "info", "warn", "error", "invalid-level"}
	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			// Just ensure it doesn't panic
			setLogLevel(level)
		})
	}
}

func TestWaitForFile(t *testing.T) {
	t.Parallel()

	t.Run("file exists immediately", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "test.xlsx")
		if err := os.WriteFile(tmpFile, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		info, err := waitForFile(tmpFile, 3, 10*time.Millisecond)
		if err != nil {
			t.Fatalf("waitForFile() error = %v", err)
		}
		if info == nil {
			t.Fatal("expected non-nil FileInfo")
		}
		if info.Size() != 4 {
			t.Errorf("expected file size 4, got %d", info.Size())
		}
	})

	t.Run("file does not exist", func(t *testing.T) {
		_, err := waitForFile("/nonexistent/path/file.xlsx", 2, 10*time.Millisecond)
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})

	t.Run("file appears after retry", func(t *testing.T) {
		dir := t.TempDir()
		tmpFile := filepath.Join(dir, "delayed.xlsx")

		go func() {
			time.Sleep(25 * time.Millisecond)
			_ = os.WriteFile(tmpFile, []byte("hello"), 0644)
		}()

		info, err := waitForFile(tmpFile, 5, 20*time.Millisecond)
		if err != nil {
			t.Fatalf("waitForFile() error = %v", err)
		}
		if info == nil {
			t.Fatal("expected non-nil FileInfo")
		}
	})
}
