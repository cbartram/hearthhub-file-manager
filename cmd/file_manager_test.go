package cmd

import (
	"flag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"os"
	"path/filepath"
	"testing"
)

type MockArchiveHandler struct {
	mock.Mock
}

func (m *MockArchiveHandler) UnzipFile() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockArchiveHandler) RemoveFilesFromZip() error {
	args := m.Called()
	return args.Error(0)
}

func TestMakeFileManager(t *testing.T) {
	t.Run("should create a file manager", func(t *testing.T) {
		args := []string{"-discord_id", "id", "-refresh_token", "token", "-prefix", "/prefix/file.zip",
			"-destination", "/valheim/plugins/", "-archive", "true", "-op", "write"}

		flagSet := flag.NewFlagSet("test", flag.ContinueOnError)

		manager, err := MakeFileManager(flagSet, args)
		assert.Nil(t, err)
		assert.True(t, manager.Archive)
		assert.Equal(t, "id", manager.DiscordId)
		assert.Equal(t, "token", manager.RefreshToken)
		assert.Equal(t, "/valheim/plugins/", manager.Destination)
		assert.Equal(t, "/prefix/file.zip", manager.Prefix)
		assert.Equal(t, "write", manager.Op)
		assert.Equal(t, "file.zip", manager.FileName)
		assert.Equal(t, "/valheim/plugins/file.zip", manager.FileDestinationPath)
	})

	t.Run("dest missing end slash", func(t *testing.T) {
		args := []string{"-discord_id", "id", "-refresh_token", "token", "-prefix", "/prefix/file.zip",
			"-destination", "/valheim/plugins", "-archive", "true", "-op", "write"}

		flagSet := flag.NewFlagSet("test", flag.ContinueOnError)

		manager, err := MakeFileManager(flagSet, args)
		assert.Nil(t, err)
		assert.True(t, manager.Archive)
		assert.Equal(t, "id", manager.DiscordId)
		assert.Equal(t, "token", manager.RefreshToken)
		assert.Equal(t, "/valheim/plugins", manager.Destination)
		assert.Equal(t, "/prefix/file.zip", manager.Prefix)
		assert.Equal(t, "write", manager.Op)
		assert.Equal(t, "file.zip", manager.FileName)
		// The final destination remains the same which is key in this test
		assert.Equal(t, "/valheim/plugins/file.zip", manager.FileDestinationPath)
	})

	tests := []struct {
		name        string
		args        []string
		expectError bool
	}{
		{
			name:        "valid input",
			args:        []string{"-discord_id=123", "-refresh_token=abc", "-prefix=file.zip", "-destination=/data", "-archive=true", "-op=write"},
			expectError: false,
		},
		{
			name:        "no archive",
			args:        []string{"-discord_id=123", "-refresh_token=abc", "-prefix=file.zip", "-destination=/data", "-archive=false", "-op=write"},
			expectError: false,
		},
		{
			name:        "missing discord_id",
			args:        []string{"-refresh_token=abc", "-prefix=file.zip", "-destination=/data", "-archive=true", "-op=delete"},
			expectError: true,
		},
		{
			name:        "invalid op",
			args:        []string{"-discord_id=123", "-refresh_token=abc", "-prefix=file.zip", "-destination=/data", "-archive=true", "-op=invalid"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
			_, err := MakeFileManager(flagSet, tt.args)

			if tt.expectError && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestDoOperation(t *testing.T) {
	// Create a temporary directory for our tests
	tempDir, err := os.MkdirTemp("", "filemanager-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name          string
		fileManager   *FileManager
		setupFunc     func(t *testing.T, tempDir string) error
		expectedError bool
		checkFunc     func(t *testing.T, tempDir string) error
	}{
		{
			name: "Write non-archive file",
			fileManager: &FileManager{
				Op:                  "write",
				Archive:             false,
				FileDestinationPath: filepath.Join(tempDir, "test.txt"),
				ArchiveHandler:      &Archive{},
			},
			setupFunc: func(t *testing.T, tempDir string) error {
				// Create a test file
				return os.WriteFile(filepath.Join(tempDir, "test.txt"), []byte("test content"), 0644)
			},
			expectedError: false,
			checkFunc: func(t *testing.T, tempDir string) error {
				// File should exist since write operation on non-archive just keeps the file
				_, err := os.Stat(filepath.Join(tempDir, "test.txt"))
				return err
			},
		},
		{
			name: "Delete non-archive file",
			fileManager: &FileManager{
				Op:                  "delete",
				Archive:             false,
				FileDestinationPath: filepath.Join(tempDir, "test.txt"),
				ArchiveHandler:      &Archive{},
			},
			setupFunc: func(t *testing.T, tempDir string) error {
				// Create a test file that should be deleted
				return os.WriteFile(filepath.Join(tempDir, "test.txt"), []byte("test content"), 0644)
			},
			expectedError: false,
			checkFunc: func(t *testing.T, tempDir string) error {
				// File should not exist after delete
				_, err := os.Stat(filepath.Join(tempDir, "test.txt"))
				if err == nil {
					return os.ErrExist
				}
				if os.IsNotExist(err) {
					return nil
				}
				return err
			},
		},
		{
			name: "Delete non-existent file",
			fileManager: &FileManager{
				Op:                  "delete",
				Archive:             false,
				FileDestinationPath: filepath.Join(tempDir, "nonexistent.txt"),
				ArchiveHandler:      &Archive{},
			},
			setupFunc:     nil,
			expectedError: true,
			checkFunc:     nil,
		},
		{
			name: "Write archive file",
			fileManager: &FileManager{
				Op:                  "write",
				Archive:             true,
				FileDestinationPath: filepath.Join(tempDir, "test.zip"),
				Destination:         tempDir,
				ArchiveHandler: &Archive{
					ZipFilePath: filepath.Join(tempDir, "test.zip"),
					Destination: tempDir,
				},
			},
			setupFunc: func(t *testing.T, tempDir string) error {
				// Create a mock Archive.UnzipFile method
				// In a real test, you would create a test zip file here
				return nil
			},
			expectedError: true, // Will fail because we haven't created a real zip file
			checkFunc:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			if tt.setupFunc != nil {
				err := tt.setupFunc(t, tempDir)
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
			}

			// Execute
			err := tt.fileManager.DoOperation()

			// Check error expectation
			if tt.expectedError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectedError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Additional checks
			if tt.checkFunc != nil {
				if err := tt.checkFunc(t, tempDir); err != nil {
					t.Errorf("Check failed: %v", err)
				}
			}
		})
	}
}

func TestDirExists(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "filemanager-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp directory: %v", err)
		}
		defer os.RemoveAll(tempDir)

		f := FileManager{}
		assert.True(t, f.DirExists(tempDir))
	})

	t.Run("not exists", func(t *testing.T) {
		f := FileManager{}
		assert.False(t, f.DirExists(filepath.Join("/nonexistent")))
	})

	t.Run("not a dir", func(t *testing.T) {
		f := FileManager{}
		assert.False(t, f.DirExists(filepath.Join("./nonexistent.txt")))
	})
}
