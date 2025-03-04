package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/cbartram/hearthhub-common/model"
	"github.com/cbartram/hearthhub-common/service"
	"github.com/cbartram/hearthhub-file-manager/cmd"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm/clause"
	"os"
	"path/filepath"
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

	log.Infof("sleeping for 7 seconds to allow server to terminate")
	time.Sleep(7 * time.Second)

	if !fileManager.DirExists(modPath) || !fileManager.DirExists(configPath) || !fileManager.DirExists(backupsPath) {
		log.Fatal("required conf, backup, or mod directory does not exist")
	}

	db := model.Connect()
	s3Client := cmd.MakeS3Client(cfg)
	err = s3Client.DownloadFile(fileManager)
	if err != nil {
		log.Fatalf("failed to download file: %v", err)
	}
	err = cmd.SyncWorldFiles(s3Client, fileManager)
	if err != nil {
		log.Errorf("failed to sync world files: %v", err)
	}

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
	cognito := service.MakeCognitoService(cfg)
	_, err = cognito.AuthUser(context.Background(), &fileManager.RefreshToken, &fileManager.DiscordId, db)
	if err != nil {
		log.Fatalf("failed to authenticate user: %v", err)
	}

	var user model.User
	db.Where("discord_id = ?", fileManager.DiscordId).First(&user)

	var size int64 = 0
	f, err := os.Stat(fileManager.FileDestinationPath)
	if err == nil {
		size = f.Size()
	}

	if fileManager.Archive {
		user.ModFiles = append(user.ModFiles, model.ModFile{
			BaseFile: model.BaseFile{
				UserID:    user.ID,
				Size:      size,
				FileName:  fileManager.FileName,
				Installed: fileManager.Op == cmd.WRITE || fileManager.Op == cmd.COPY,
				S3Key:     fileManager.Prefix,
			},
			UpVotes:            0,
			Downloads:          0,
			OriginalUploadDate: time.Now(),
			LatestUploadDate:   time.Now(),
			Creator:            "",
			HeroImage:          "",
			Description:        "",
		})

		for _, file := range user.ModFiles {
			db.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "file_name"}},
				DoUpdates: clause.AssignmentColumns([]string{"installed", "size"}),
			}).Create(&file)
		}
	}

	if strings.HasSuffix(fileManager.FileDestinationPath, ".fwl") || strings.HasSuffix(fileManager.FileDestinationPath, ".db") {
		// Allow only files which are not *_backup_auto-* since those files are replica backups there's no badge for install status
		// on the UI for them and therefore they don't need to be stored in cognito wasting space.
		backups, err := fileManager.ListFiles(cmd.BACKUPS_DIR, func(fileName string) bool {
			return filepath.Ext(fileName) == ".db" || filepath.Ext(fileName) == ".fwl"
		})

		if err != nil {
			log.Fatalf("failed to list backup files: %v", err)
		}

		for _, file := range backups {
			if !strings.Contains(file.Name(), "_backup_auto-") {
				user.WorldFiles = append(user.WorldFiles, model.WorldFile{
					BaseFile: model.BaseFile{
						UserID:    user.ID,
						Size:      size,
						FileName:  filepath.Base(file.Name()),
						S3Key:     fmt.Sprintf("valheim-backups-auto/%s/%s", user.DiscordID, filepath.Base(file.Name())),
						Installed: fileManager.Op == cmd.WRITE || fileManager.Op == cmd.COPY,
					},
				})
			} else {
				user.BackupFiles = append(user.BackupFiles, model.BackupFile{
					BaseFile: model.BaseFile{
						UserID:    user.ID,
						Size:      size,
						FileName:  filepath.Base(file.Name()),
						S3Key:     fmt.Sprintf("valheim-backups-auto/%s/%s", user.DiscordID, filepath.Base(file.Name())),
						Installed: fileManager.Op == cmd.WRITE || fileManager.Op == cmd.COPY,
					},
				})
			}
		}

		for _, backup := range user.BackupFiles {
			db.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "file_name"}},
				DoUpdates: clause.AssignmentColumns([]string{"s3_key", "installed"}),
			}).Create(&backup)
		}

		for _, world := range user.WorldFiles {
			db.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "file_name"}},
				DoUpdates: clause.AssignmentColumns([]string{"s3_key", "installed"}),
			}).Create(&world)
		}
	}

	if isConfigFile(fileManager.FileDestinationPath) {
		user.ConfigFiles = append(user.ConfigFiles, model.ConfigFile{
			BaseFile: model.BaseFile{
				UserID:    user.ID,
				Size:      size,
				FileName:  fileManager.FileName,
				S3Key:     fileManager.Prefix,
				Installed: fileManager.Op == cmd.WRITE || fileManager.Op == cmd.COPY,
			},
		})

		for _, c := range user.ConfigFiles {
			db.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "file_name"}},
				DoUpdates: clause.AssignmentColumns([]string{"s3_key", "installed"}),
			}).Create(&c)
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
	db.Save(&user)
	log.Infof("done.")
}

func isConfigFile(path string) bool {
	return strings.HasSuffix(path, ".cfg") || strings.HasSuffix(path, ".json") || strings.HasSuffix(path, ".yaml")
}
