package cmd

import (
	"errors"
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
	FileDestinationPath string // The path on PVC which includes the destination and file name i.e /Valheim/BepInEx/plugins/Mod.zip
	ArchiveHandler      *Archive
}

func MakeFileManager(flagSet *flag.FlagSet, args []string) (*FileManager, error) {
	var discordId, refreshToken, prefix, destination, archive, op string
	flagSet.StringVar(&discordId, "discord_id", "", "Discord ID")
	flagSet.StringVar(&refreshToken, "refresh_token", "", "Refresh token")
	flagSet.StringVar(&prefix, "prefix", "", "S3 prefix name including the extension. ex: file.zip")
	flagSet.StringVar(&destination, "destination", "", "PVC volume destination")
	flagSet.StringVar(&archive, "archive", "", "If the file being downloaded is an archive and needs unpacked.")
	flagSet.StringVar(&op, "op", "", "Operation to perform either \"write\" or \"delete\"")

	// Parse flags
	if err := flagSet.Parse(args); err != nil {
		return nil, fmt.Errorf("failed to parse flags: %v", err)
	}

	if op != "write" && op != "delete" {
		return nil, errors.New("invalid \"op\" argument specified. Must be one of: write, delete")
	}

	var isArchive bool
	if archive == "true" {
		log.Infof("given file: %s is an archive and needs unpacked.", prefix)
		isArchive = true
	} else {
		isArchive = false
	}

	if discordId == "" || refreshToken == "" {
		return nil, errors.New("-discord_id and -refresh_token args are required")
	}

	log.Infof("Discord ID: %s, file name: %s, destination: %s is_archive: %v operation: %s", discordId, prefix, destination, isArchive, op)

	fileName := filepath.Base(prefix)
	temporaryDestination := destination
	if !strings.HasSuffix(temporaryDestination, "/") {
		temporaryDestination += "/"
	}

	finalPath := fmt.Sprintf("%s%s", temporaryDestination, fileName)
	return &FileManager{
		DiscordId:           discordId,
		RefreshToken:        refreshToken,
		Prefix:              prefix,
		Destination:         destination,
		Archive:             isArchive,
		Op:                  op,
		FileName:            fileName,
		FileDestinationPath: finalPath,
		ArchiveHandler: &Archive{
			ZipFilePath: finalPath,
			Destination: destination,
		},
	}, nil
}

// DoOperation Performs the desired operation specified in the "op" flag. This will either unpack a zip to the
// specified destination or search through the zip and remove all files corresponding to the zip at the specified
// destination. This ensures mods can be uninstalled without having to keep track of which files belong to which
// mod with any type of manifest. The zip file for the mod tracks this info for us!
func (f *FileManager) DoOperation() error {
	if f.Op == "write" {
		if f.Archive {
			// Unpack the file from /valheim/BepInEx/plugins/ValheimPlus.zip to /valheim/BepInEx/plugins/
			err := f.ArchiveHandler.UnzipFile()
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
			err := f.ArchiveHandler.RemoveFilesFromZip()
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
