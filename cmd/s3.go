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
	"strings"
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

// SyncWorldFiles Synchronizes a .db or .fwl file along with its pair to disk. I.e. if the prefix for the file
// in s3 ends with .db this will also download the corresponding .fwl file and vice versa. This ensures that world
// file stay synchronized between S3 and the pvc.
func SyncWorldFiles(s3Client *S3Client, fileManager *FileManager) error {
	var tmpManager FileManager
	if strings.HasSuffix(fileManager.Prefix, ".db") {
		log.Infof("file is a *.db, syncing paired *.fwl")
		tmpManager = FileManager{
			Prefix:              fmt.Sprintf("%s%s", strings.TrimSuffix(fileManager.Prefix, ".db"), ".fwl"),
			FileName:            fmt.Sprintf("%s%s", strings.TrimSuffix(fileManager.FileName, ".db"), ".fwl"),
			FileDestinationPath: fmt.Sprintf("%s%s", strings.TrimSuffix(fileManager.FileDestinationPath, ".db"), ".fwl"),
		}
	} else if strings.HasSuffix(fileManager.Prefix, ".fwl") {
		log.Infof("file is a *.fwl, syncing paired *.db")
		tmpManager = FileManager{
			Prefix:              fmt.Sprintf("%s%s", strings.TrimSuffix(fileManager.Prefix, ".fwl"), ".db"),
			FileName:            fmt.Sprintf("%s%s", strings.TrimSuffix(fileManager.FileName, ".fwl"), ".db"),
			FileDestinationPath: fmt.Sprintf("%s%s", strings.TrimSuffix(fileManager.FileDestinationPath, ".fwl"), ".db"),
		}
	} else {
		log.Infof("file: %s is not a world file. skipping sync", fileManager.Prefix)
		return nil
	}

	err := s3Client.DownloadFile(&tmpManager)
	if err != nil {
		return err
	}

	log.Infof("synced world file: %s to: %s", tmpManager.Prefix, tmpManager.FileDestinationPath)
	return nil
}
