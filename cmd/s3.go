package cmd

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	log "github.com/sirupsen/logrus"
	"io"
	"os"
)

type S3Client struct {
	BucketName string
	client     ObjectStore
}

type ObjectStore interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// MakeS3Client Creates a new S3 Client object.
func MakeS3Client(cfg aws.Config) *S3Client {
	return &S3Client{
		BucketName: os.Getenv("BUCKET_NAME"),
		client:     s3.NewFromConfig(cfg),
	}
}

// DownloadFile Downloads a file (zip, config, world save or otherwise) from S3 and writes it to the specified destination on disk.
// This function does not unzip the file.
func (s *S3Client) DownloadFile(fileManager *FileManager) error {
	result, err := s.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(s.BucketName),
		Key:    aws.String(fileManager.Prefix),
	})
	if err != nil {
		return errors.New(fmt.Sprintf("failed to get object %v:%v err: %v", s.BucketName, fileManager.Prefix, err))
	}

	defer result.Body.Close()

	log.Infof("creating file with name: %s in %s", fileManager.FileName, fileManager.FileDestinationPath)
	file, err := os.Create(fileManager.FileDestinationPath)

	if err != nil {
		log.Errorf("failed to create file %v err: %v", fileManager.Prefix, err)
		return err
	}

	defer file.Close()
	body, err := io.ReadAll(result.Body)

	if err != nil {
		log.Errorf("failed to read object body from %v error: %v", fileManager.Prefix, err)
		return err
	}
	_, err = file.Write(body)

	return err
}
