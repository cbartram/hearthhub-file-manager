package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/cbartram/hearthhub-plugin-manager/cmd"
	log "github.com/sirupsen/logrus"
	"os"
	"strings"
	"time"
)

const (
	modPath     = "/valheim/BepInEx/plugins/"
	backupsPath = "/root/.config/unity3d/IronGate/Valheim/worlds_local/"
	configPath  = "/valheim/BepInEx/config/"
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

	if !fileManager.DirExists(modPath) || !fileManager.DirExists(configPath) || !fileManager.DirExists(backupsPath) {
		log.Fatal("required conf, backup, or mod directory does not exist")
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

	rabbit, err := cmd.MakeRabbitMQService()
	if err != nil {
		log.Fatalf("failed to make rabbitmq service: %v", err)
	}

	// TODO: In the future consider publishing failure messages as well.
	err = rabbit.PublishMessage(&cmd.Message{
		Type:      "PreStop",
		Body:      fmt.Sprintf(`{"containerName": "%s", "operation": "%s", "containerType": "file-install"}`, os.Getenv("HOSTNAME"), fileManager.Op),
		DiscordId: fileManager.DiscordId,
	})
	cognito := cmd.MakeCognitoService(cfg)
	user, err := cognito.AuthUser(context.Background(), &fileManager.RefreshToken, &fileManager.DiscordId)
	if err != nil {
		log.Fatalf("failed to authenticate user: %v", err)
	}

	// This check lets us know this is indeed a mod file which has been installed
	// Therefore, we need to update the user custom:installed_mods with the file that was installed or uninstalled
	if fileManager.Archive {
		err = cognito.MergeInstalledFiles(context.Background(), user, fileManager.FileName, "custom:installed_mods", fileManager.Op)
		if err != nil {
			log.Fatalf("failed to merge installed mods: %v", err)
		}
	}

	// It was a backup file that was installed
	if strings.HasSuffix(fileManager.FileDestinationPath, ".fwl") || strings.HasSuffix(fileManager.FileDestinationPath, ".db") {
		err = cognito.MergeInstalledBackups(ctx, user, fileManager.FileName, fileManager.Op)
		if err != nil {
			log.Fatalf("failed to merge installed backups: %v", err)
		}
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
