package main

import (
	"cbartram/hearthhub-plugin-manager/cmd"
	"context"
	"github.com/aws/aws-sdk-go-v2/config"
	log "github.com/sirupsen/logrus"
	"os"
	"time"
)

const (
	pluginsPath = "/valheim/BepInEx/plugins/"
)

func main() {
	ctx := context.Background()
	logger := log.New()
	logger.SetFormatter(&log.TextFormatter{
		FullTimestamp: false,
	})

	logLevel, err := log.ParseLevel(os.Getenv("LOG_LEVEL"))
	if err != nil {
		logLevel = log.InfoLevel
	}

	log.SetOutput(os.Stdout)
	log.SetLevel(logLevel)

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("unable to load AWS SDK config: %v", err)
	}

	fileManager := cmd.MakeFileManager()
	hearthhubClient := cmd.MakeHearthHubClient(os.Getenv("API_BASE_URL"))

	err = hearthhubClient.ScaleDeployment(fileManager, 0)
	if err != nil {
		log.Fatalf("failed to scale valheim server deployment: %v", err)
	}

	// TODO Don't love this I'd rather poll the API to find out when the server is in termination status
	log.Infof("sleeping for 15 seconds to allow server to terminate")
	time.Sleep(15 * time.Second)

	if !fileManager.DirExists(pluginsPath) {
		log.Fatal("plugins directory does not exist")
	}

	// Download and unzip the mod file from S3 unpacking it onto the PVC
	s3Client := cmd.MakeS3Client(cfg)
	err = s3Client.DownloadFile(fileManager)
	if err != nil {
		log.Fatalf("failed to download file: %v", err)
	}

	log.Infof("file: %s downloaded successfully to: %s for user: %s", fileManager.Prefix, fileManager.FileDestinationPath, fileManager.DiscordId)

	err = fileManager.DoOperation()
	if err != nil {
		log.Fatalf("failed to unpack or remove files: %v", err)
	}

	// Re-scale up the server
	err = hearthhubClient.ScaleDeployment(fileManager, 1)
	if err != nil {
		log.Fatalf("failed to scale deployment back to 1: %v", err)
	}
	log.Infof("valheim server deployment scaled to 1. Done.")
}
