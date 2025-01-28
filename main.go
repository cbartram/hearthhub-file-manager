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

const (
	pluginsPath = "/valheim/BepInEx/plugins/"
	bucketName  = "hearthhub-backups"
)

type ScaleDeploymentPayload struct {
	DiscordId    string `json:"discord_id"`
	RefreshToken string `json:"refresh_token"`
	Replicas     int    `json:"replicas"`
}

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

	// The prefix for the object in S3. i.e. /mods/general/ValheimPlus.zip
	prefix := flag.String("prefix", "", "S3 prefix name including the extension. ex: file.zip")

	// The destination to write the file to i.e /valheim/BepInEx/plugins This path does NOT need to include the file name as it
	// will be parsed from the prefix automatically.
	destination := flag.String("destination", "", "PVC volume destination")

	// True if the file is an archive and needs to be unpacked in the destination as well
	archive := flag.String("archive", "", "If the file being downloaded is an archive and needs unpacked.")

	// This determines if the job writes and unpacks files it gets from S3 or if it reads the files in the zip file and deletes
	// the files in the destination dir.
	op := flag.String("op", "", "Operation to perform either \"write\" or \"delete\"")
	flag.Parse()

	if *op != "write" && *op != "delete" {
		logger.Fatal("Invalid \"op\" argument specified. Must be one of: write, delete")
		return
	}

	var isArchive bool
	if *archive == "true" {
		log.Infof("given file: %s is an archive and needs unpacked.", *prefix)
		isArchive = true
	} else {
		isArchive = false
	}

	if *discordId == "" || *refreshToken == "" {
		log.Fatalf("-discord_id and -refresh_token args are required")
	}

	log.Infof("Discord ID: %s, file name: %s, destination: %s", *discordId, *prefix, *destination)

	err = ScaleDeployment(*discordId, *refreshToken, 0)
	if err != nil {
		log.Fatal("failed to scale valheim server deployment: %v", err)
	}

	// TODO Don't love this I'd rather poll the API to find out when the server is in termination status
	log.Infof("sleeping for 15 seconds to allow server to terminate")
	time.Sleep(15 * time.Second)

	if !PluginsDirExists() {
		log.Fatal("plugins directory does not exist")
	}

	log.Infof("plugin directory exists on PVC")

	// Download and unzip the mod file from S3 unpacking it onto the PVC
	s3Client, err := MakeS3Client(ctx)
	if err != nil {
		log.Fatalf("failed to make S3 client: %v", err)
	}

	err = DownloadFile(ctx, s3Client, *prefix, *destination)
	if err != nil {
		log.Fatalf("failed to download file: %v", err)
	}

	log.Infof("file: %s downloaded successfully for user: %s", *prefix, *discordId)

	fileName := filepath.Base(*prefix)
	finalDestination := fmt.Sprintf("%s/%s", *destination, fileName)

	if *op == "write" {
		if isArchive {
			// Unpack the file from /valheim/BepInEx/plugins/ValheimPlus.zip to /valheim/BepInEx/plugins/
			err = UnzipFile(finalDestination, *destination)
			if err != nil {
				log.Fatalf("failed to unzip file: %s err: %v", finalDestination, err)
			}
			log.Infof("file unzipped to: %s", *destination)
		} else {
			log.Infof("skipping unpack for %s", finalDestination)
		}
	} else {
		log.Infof("job is a delete operation")
		err = RemoveFilesFromZip(finalDestination, *destination)
		if err != nil {
			log.Errorf("failed to remove files from zip: %v", err)
			return
		}
	}

	// Re-scale up the server
	err = ScaleDeployment(*discordId, *refreshToken, 1)
	if err != nil {
		log.Fatalf("failed to scale deployment back to 1: %v", err)
	}

	log.Infof("valheim server deployment scaled to 1. Done.")
}

// RemoveFilesFromZip Removes all the files that are present in a zip file from the destination as well as the zip file itself.
// This function reads the zip file to determine which files to delete and is used for mod uninstallation.
func RemoveFilesFromZip(zipFilePath, destination string) error {
	zipReader, err := zip.OpenReader(zipFilePath)
	if err != nil {
		log.Fatalf("Failed to open ZIP file: %v", err)
	}
	defer zipReader.Close()

	// Iterate over the files in the ZIP and remove them from the PVC
	for _, f := range zipReader.File {
		filePath := filepath.Join(destination, f.Name)
		log.Infof("removing file %s", filePath)
		if err := os.Remove(filePath); err != nil {
			if os.IsNotExist(err) {
				log.Infof("file %s does not exist, skipping...", filePath)
				continue
			}
			log.Errorf("Failed to remove file %s: %v", filePath, err)
		}
	}

	if err := os.Remove(zipFilePath); err != nil {
		log.Fatalf("failed to remove temporary ZIP file: %v", err)
	}
	return nil
}

// DownloadFile Downloads a mod zip file from S3 and writes it to disk. This function does not unzip the file.
func DownloadFile(ctx context.Context, s3Client *s3.Client, prefix, destination string) error {
	result, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(prefix),
	})
	if err != nil {
		var noKey *types.NoSuchKey
		if errors.As(err, &noKey) {
			log.Printf("can't get object %s from bucket %s. no such key exists", prefix, bucketName)
			err = noKey
		} else {
			log.Infof("failed to get object %v:%v err: %v", bucketName, prefix, err)
		}
		return err
	}

	defer result.Body.Close()

	// For zip files (mods) this is a bit redundant as both the zip file and the dll file for the mod
	// will be present however, BepInEx should be smart enough to pick up only the DLL files. Having extra zip files
	// on the PVC shouldn't be an issue.
	fileName := filepath.Base(prefix)

	if !strings.HasSuffix(destination, "/") {
		destination += "/"
	}

	finalPath := fmt.Sprintf("%s%s", destination, fileName)
	log.Infof("creating file with name: %s in %s", fileName, finalPath)
	file, err := os.Create(finalPath)

	if err != nil {
		log.Errorf("failed to create file %v err: %v", prefix, err)
		return err
	}

	defer file.Close()
	body, err := io.ReadAll(result.Body)

	if err != nil {
		log.Errorf("failed to read object body from %v error: %v", prefix, err)
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
	url := "http://hearthhub-mod-api.hearthhub.svc.cluster.local:8080/api/v1/server/scale"
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

	log.Infof("PUT /api/v1/server/scale: %v response: %s", scale, bodyString)
	if res.StatusCode != 200 {
		// If server is already scaled to 0 we get a 400 status code, but it's already in the state I want.
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

	// Cleanup
	defer os.Remove(zipFile)

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
