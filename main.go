package main

import (
	"context"
	"flag"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/cbartram/hearthhub-plugin-manager/cmd"
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

	flagSet := flag.NewFlagSet("file-manager", flag.ExitOnError)
	fileManager, err := cmd.MakeFileManager(flagSet, os.Args[1:])
	if err != nil {
		log.Fatalf("unable to make file manager: %v", err)
	}
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
	err = cmd.SyncWorldFiles(s3Client, fileManager)
	if err != nil {
		log.Errorf("failed to sync world files: %v", err)
	}

	log.Infof("file: %s downloaded successfully to: %s for user: %s", fileManager.Prefix, fileManager.FileDestinationPath, fileManager.DiscordId)

	err = fileManager.DoOperation()
	if err != nil {
		log.Fatalf("failed to unpack or remove files: %v", err)
	}

	// Scaling the server back up has been disabled because
	// - Users can select a different world or modify server args after a mod/world/config is installed
	// - Allows users to install multiple mods, files, config, saves without the server having to spin up and down every time
	// - Once a user is fully done configuring their server they can spin it up once with the PUT /api/v1/server/scale code

	//err = hearthhubClient.ScaleDeployment(fileManager, 1)
	//if err != nil {
	//	log.Fatalf("failed to scale deployment back to 1: %v", err)
	//}
	log.Infof("done.")
}
