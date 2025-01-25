package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Plugin struct {
	ID        int       `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

const (
	pluginsPath = "/valheim/BepInEx/plugins/"
	bucketName  = "hearthhub-backups"
)

type ScaleDeploymentPayload struct {
	DiscordId    string `json:"discord_id"`
	RefreshToken string `json:"refresh_token"`
	Replicas     int    `json:"replicas"`
}

// 4. Pull mod from S3
// 5. Unpack mod onto pvc
// 6. Scale up deployment
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

	discordId := flag.String("discord_id", "", "Discord ID")
	refreshToken := flag.String("refresh_token", "", "Refresh token")
	modName := flag.String("mod_name", "", "Valheim mod name")
	// True if the mod is a custom upload from the user, false if its a widely available and selectable mod
	personalMod := flag.Bool("personal_mod", false, "Personal Mod")
	flag.Parse()

	if *discordId == "" || *refreshToken == "" {
		log.Fatalf("-discord_id and -refresh_token args are required")
	}

	err = ScaleDeployment(*discordId, *refreshToken, 0)
	if err != nil {
		log.Fatal("failed to scale valheim server deployment: %v", err)
	}

	if !PluginsDirExists() {
		log.Fatal("plugins directory does not exist")
	}

	log.Infof("plugin directory exists on PVC")

	// Download and unzip the mod file from S3 unpacking it onto the PVC
	s3Client, err := MakeS3Client(ctx)
	if err != nil {
		log.Fatalf("failed to make S3 client: %v", err)
	}

	err = DownloadFile(ctx, s3Client, *modName, *discordId, *personalMod)
	if err != nil {
		log.Fatalf("failed to download plugin: %v", err)
	}

	log.Infof("mod file: %s downloaded successfully for user: %s", *modName, *discordId)

	err = UnzipFile(fmt.Sprintf("%s.zip", *modName), pluginsPath)
	if err != nil {
		log.Fatalf("failed to unzip plugin: %v", err)
	}

	// Re-scale up the server
	err = ScaleDeployment(*discordId, *refreshToken, 1)
	if err != nil {
		log.Fatalf("failed to scale deployment back to 1: %v", err)
	}

	log.Infof("Done.")
}

// DownloadFile Downloads a mod zip file from S3 and writes it to disk. This function does not unzip the file.
func DownloadFile(ctx context.Context, s3Client *s3.Client, modName, discordId string, personalMod bool) error {
	var key string
	if personalMod {
		key = fmt.Sprintf("mods/%s/%s.zip", discordId, modName)
	} else {
		key = fmt.Sprintf("mods/general/%s.zip", modName)
	}

	result, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		var noKey *types.NoSuchKey
		if errors.As(err, &noKey) {
			log.Printf("can't get object %s from bucket %s. no such key exists", key, bucketName)
			err = noKey
		} else {
			log.Infof("failed to get object %v:%v err: %v", bucketName, key, err)
		}
		return err
	}

	defer result.Body.Close()
	file, err := os.Create(fmt.Sprintf("%s.zip", modName))

	if err != nil {
		log.Errorf("failed to create zip file %v.zip err: %v", modName, err)
		return err
	}

	defer file.Close()
	body, err := io.ReadAll(result.Body)

	if err != nil {
		log.Errorf("failed to read object body from %v error: %v", key, err)
	}
	_, err = file.Write(body)
	return err
}

// MakeS3Client Creates a new S3 Client object.
func MakeS3Client(ctx context.Context) (*s3.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS SDK config: %v", err)
	}

	return s3.NewFromConfig(cfg), nil
}

// PluginsDirExists Checks for the presence of the plugins directory on the (assumed) mounted PVC.
func PluginsDirExists() bool {
	// Verify that the PVC is mounted on the correct path:
	info, err := os.Stat(pluginsPath)
	if os.IsNotExist(err) {
		log.Errorf("%s directory does not exist. is pvc mounted?", pluginsPath)
		return false
	}

	if err != nil {
		log.Errorf("failed to stat %s: %v", pluginsPath, err)
		return false
	}

	if !info.IsDir() {
		log.Errorf("%s is not a directory", pluginsPath)
		return false
	}
	return true
}

// ScaleDeployment Scales a Kubernetes Valheim Dedicated Server Deployment to either 1 or 0.
func ScaleDeployment(discordId, refreshToken string, scale int) error {
	url := "http://hearthhub-mod-api.hearthhub.svc.cluster.local/api/v1/server/scale"
	method := "PUT"

	scaleDeploymentObj := ScaleDeploymentPayload{
		DiscordId:    discordId,
		RefreshToken: refreshToken,
		Replicas:     scale,
	}

	payload, err := json.Marshal(scaleDeploymentObj)
	if err != nil {
		return err
	}

	client := &http.Client{}
	req, err := http.NewRequest(method, url, bytes.NewReader(payload))

	if err != nil {
		return err
	}

	res, err := client.Do(req)

	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	bodyString := string(body)

	if res.StatusCode != 200 {
		// If server is already scaled to 0 we get a 400 status code but it's already in the state I want.
		if scale == 0 && res.StatusCode == 400 && strings.Contains(bodyString, "no server to terminate") {
			return nil
		}

		if scale == 1 && res.StatusCode == 400 && strings.Contains(bodyString, "server already running") {
			return nil
		}
		return fmt.Errorf("failed to scale replica to: %v, status code: %v, body: %s", scale, res.StatusCode, bodyString)
	}
	return nil
}

func UnzipFile(zipFile, dest string) error {
	reader, err := zip.OpenReader(zipFile)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		path := filepath.Join(dest, file.Name)

		if file.FileInfo().IsDir() {
			os.MkdirAll(path, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}

		outFile, err := os.Create(path)
		if err != nil {
			return err
		}

		rc, err := file.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}

	return nil
}
