package cmd

import (
	"bytes"
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"testing"
)

type MockS3Client struct {
	mock.Mock
}

func (m *MockS3Client) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	args := m.Called(ctx, params, optFns)
	return args.Get(0).(*s3.GetObjectOutput), args.Error(1)
}

func TestMakeS3Client(t *testing.T) {
	cfg := aws.Config{}
	os.Setenv("BUCKET_NAME", "FOO")

	client := MakeS3Client(cfg)
	assert.NotNil(t, client)
	assert.Equal(t, client.BucketName, "FOO")
}

func TestDownloadFile_Success(t *testing.T) {
	// Setup
	mockS3 := new(MockS3Client)
	tmp, err := os.CreateTemp("", "test-*.zip")
	if err != nil {
		t.Fatalf("Failed to create temp zip: %v", err)
	}

	defer os.RemoveAll(tmp.Name())

	fileManager := &FileManager{
		Prefix:              "test-key",
		FileName:            tmp.Name(),
		FileDestinationPath: tmp.Name(),
	}
	s3Client := &S3Client{
		BucketName: "test-bucket",
		client:     mockS3,
	}

	mockS3.On("GetObject", mock.Anything, &s3.GetObjectInput{
		Bucket: aws.String("test-bucket"),
		Key:    aws.String("test-key"),
	}, mock.Anything).Return(&s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader([]byte("test content"))),
	}, nil)

	err = s3Client.DownloadFile(fileManager)

	require.NoError(t, err)
	mockS3.AssertExpectations(t)

	content, err := os.ReadFile(tmp.Name())
	require.NoError(t, err)
	require.Equal(t, "test content", string(content))
}

func TestDownloadFile_CreateFileError(t *testing.T) {
	// Setup
	mockS3 := new(MockS3Client)
	fs := afero.NewMemMapFs()
	fileManager := &FileManager{
		Prefix:              "test-key",
		FileName:            "test-file.txt",
		FileDestinationPath: "/path/to/destination/test-file.txt",
	}
	s3Client := &S3Client{
		BucketName: "test-bucket",
		client:     mockS3,
	}

	// Mock the GetObject call
	mockS3.On("GetObject", mock.Anything, &s3.GetObjectInput{
		Bucket: aws.String("test-bucket"),
		Key:    aws.String("test-key"),
	}, mock.Anything).Return(&s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader([]byte("test content"))),
	}, nil)

	// Mock the file creation to return an error
	fs.MkdirAll("/path/to/destination", 0755)
	fs.Create("/path/to/destination/test-file.txt") // Create the file to force an error

	// Execute
	err := s3Client.DownloadFile(fileManager)

	// Assert
	require.Error(t, err)
	mockS3.AssertExpectations(t)
}
