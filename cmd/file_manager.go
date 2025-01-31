package cmd

import (
	"flag"
	"fmt"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"strings"
)

type FileManager struct {
	DiscordId           string
	RefreshToken        string
	Prefix              string // The prefix in S3: /mod-files/{user-id}/Mod.zip
	Destination         string // The destination dir /Valheim/BepInEx/plugins
	Archive             bool
	Op                  string
	FileName            string // The name of the file: Mod.zip
	FileDestinationPath string // The path on PVC which includes the destination +  file name i.e /Valheim/BepInEx/plugins/Mod.zip
}

func MakeFileManager() *FileManager {
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
		log.Fatal("Invalid \"op\" argument specified. Must be one of: write, delete")
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

	log.Infof("Discord ID: %s, file name: %s, destination: %s is_archive: %v operation: %s", *discordId, *prefix, *destination, isArchive, *op)

	fileName := filepath.Base(*prefix)
	temporaryDestination := *destination
	if !strings.HasSuffix(temporaryDestination, "/") {
		temporaryDestination += "/"
	}

	finalPath := fmt.Sprintf("%s%s", temporaryDestination, fileName)
	return &FileManager{
		DiscordId:           *discordId,
		RefreshToken:        *refreshToken,
		Prefix:              *prefix,
		Destination:         *destination,
		Archive:             isArchive,
		Op:                  *op,
		FileName:            fileName,
		FileDestinationPath: finalPath,
	}
}

func (f *FileManager) DoOperation() error {
	archive := Archive{
		ZipFilePath: f.FileDestinationPath,
		Destination: f.Destination,
	}

	if f.Op == "write" {
		if f.Archive {
			// Unpack the file from /valheim/BepInEx/plugins/ValheimPlus.zip to /valheim/BepInEx/plugins/
			err := archive.UnzipFile()
			if err != nil {
				return err
			}
			log.Infof("file unzipped to: %s", f.Destination)
		} else {
			log.Infof("skipping unpack for %s", f.FileDestinationPath)
		}
	} else {
		log.Infof("job is a delete operation: is archive: %v", f.Archive)
		if f.Archive {
			err := archive.RemoveFilesFromZip()
			if err != nil {
				return err
			}
		} else {
			err := os.Remove(f.FileDestinationPath)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// DirExists Checks for the presence of a directory on the (assumed) mounted PVC.
func (f *FileManager) DirExists(dir string) bool {
	// Verify that the PVC is mounted on the correct path:
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		log.Errorf("%s directory does not exist. is pvc mounted?", dir)
		return false
	}

	if err != nil {
		log.Errorf("failed to stat %s: %v", dir, err)
		return false
	}

	if !info.IsDir() {
		log.Errorf("%s is not a directory", dir)
		return false
	}
	return true
}
