package cmd

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func createTestZip(t *testing.T, files map[string]string) string {
	tmpZip, err := os.CreateTemp("", "test-*.zip")
	if err != nil {
		t.Fatalf("Failed to create temp zip: %v", err)
	}

	zipWriter := zip.NewWriter(tmpZip)
	for name, content := range files {
		f, err := zipWriter.Create(name)
		if err != nil {
			t.Fatalf("Failed to create file in zip: %v", err)
		}
		_, err = f.Write([]byte(content))
		if err != nil {
			t.Fatalf("Failed to write content to zip file: %v", err)
		}
	}

	err = zipWriter.Close()
	if err != nil {
		t.Fatalf("Failed to close zip writer: %v", err)
	}
	err = tmpZip.Close()
	if err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	return tmpZip.Name()
}

func createTestFiles(t *testing.T, files map[string]string, baseDir string) {
	for name, content := range files {
		path := filepath.Join(baseDir, name)
		err := os.MkdirAll(filepath.Dir(path), 0755)
		if err != nil {
			t.Fatalf("Failed to create directories: %v", err)
		}
		err = os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}
	}
}

func TestUnzipFile(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		wantErr bool
	}{
		{
			name: "basic unzip",
			files: map[string]string{
				"test.txt":       "test content",
				"dir/nested.txt": "nested content",
				"empty-dir/":     "",
			},
			wantErr: false,
		},
		{
			name:    "empty zip",
			files:   map[string]string{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for destination
			destDir, err := os.MkdirTemp("", "test-dest-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(destDir)

			// Create test zip file
			zipPath := createTestZip(t, tt.files)
			defer os.Remove(zipPath)

			a := &Archive{
				ZipFilePath: zipPath,
				Destination: destDir,
			}

			err = a.UnzipFile()
			if (err != nil) != tt.wantErr {
				t.Errorf("UnzipFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify files were extracted correctly
			for name, expectedContent := range tt.files {
				if name[len(name)-1] == '/' {
					// Skip directories
					continue
				}
				path := filepath.Join(destDir, name)
				content, err := os.ReadFile(path)
				if err != nil {
					t.Errorf("Failed to read extracted file %s: %v", path, err)
					continue
				}
				if string(content) != expectedContent {
					t.Errorf("File %s content = %s, want %s", path, content, expectedContent)
				}
			}
		})
	}
}

func TestRemoveFilesFromZip(t *testing.T) {
	tests := []struct {
		name        string
		zipFiles    map[string]string
		destFiles   map[string]string
		wantErr     bool
		wantRemains map[string]bool // files that should remain after removal
	}{
		{
			name: "remove existing files",
			zipFiles: map[string]string{
				"test.txt":       "test content",
				"dir/nested.txt": "nested content",
			},
			destFiles: map[string]string{
				"test.txt":       "test content",
				"dir/nested.txt": "nested content",
				"other.txt":      "should remain",
			},
			wantErr: false,
			wantRemains: map[string]bool{
				"other.txt": true,
			},
		},
		{
			name: "missing files",
			zipFiles: map[string]string{
				"missing.txt": "content",
			},
			destFiles: map[string]string{
				"existing.txt": "should remain",
			},
			wantErr: false,
			wantRemains: map[string]bool{
				"existing.txt": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for destination
			destDir, err := os.MkdirTemp("", "test-dest-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(destDir)

			// Create test zip file
			zipPath := createTestZip(t, tt.zipFiles)
			defer os.Remove(zipPath)

			// Create destination files
			createTestFiles(t, tt.destFiles, destDir)

			a := &Archive{
				ZipFilePath: zipPath,
				Destination: destDir,
			}

			err = a.RemoveFilesFromZip()
			if (err != nil) != tt.wantErr {
				t.Errorf("RemoveFilesFromZip() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify expected files remain and others are removed
			for name := range tt.destFiles {
				path := filepath.Join(destDir, name)
				_, err := os.Stat(path)
				exists := !os.IsNotExist(err)
				if exists != tt.wantRemains[name] {
					if tt.wantRemains[name] {
						t.Errorf("File %s should exist but doesn't", name)
					} else {
						t.Errorf("File %s shouldn't exist but does", name)
					}
				}
			}

			// Verify zip file was removed
			if _, err := os.Stat(zipPath); !os.IsNotExist(err) {
				t.Error("Zip file should have been removed")
			}
		})
	}
}
