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

var (
	COPY        = "copy"
	WRITE       = "write"
	DELETE      = "delete"
	BACKUPS_DIR = "/root/.config/unity3d/IronGate/Valheim/worlds_local/"
	PLUGINS_DIR = "/valheim/BepInEx/plugins/"
	CONFIG_DIR  = "/valheim/BepInEx/config"
)

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

	if op != WRITE && op != DELETE && op != COPY {
		return nil, errors.New("invalid \"op\" argument specified. Must be one of: write, delete, copy")
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
	var finalPath string
	temporaryDestination := destination

	if op == COPY && isArchive {
		return nil, errors.New("\"copy\" operation and archive cannot be used together")
	}

	if op != COPY {
		if !strings.HasSuffix(temporaryDestination, "/") {
			temporaryDestination += "/"
		}
		finalPath = fmt.Sprintf("%s%s", temporaryDestination, fileName)
	} else {
		// For copy op's we expect the destination to end with a file not a dir
		// therefore the finalpath will be the passed in destination overwriting the file
		if strings.HasSuffix(destination, "/") {
			temporaryDestination = strings.TrimSuffix(destination, "/")
		}
		finalPath = temporaryDestination
	}

	log.Infof("full destination path for file: %s", finalPath)
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
// mod with any type of manifest. Note: copy operations don't need special handling here since they are technically
// just write ops directed at a file rather than a dir (overwriting the file).
func (f *FileManager) DoOperation() error {
	if f.Op == WRITE || f.Op == COPY {
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
			f.ArchiveHandler.RemoveFilesFromZip()
		} else {
			// Handle removing .db and .fwl files when the op is a remove (similar to the s3 sync but opposite)
			if strings.HasSuffix(f.Prefix, ".db") {
				log.Infof("file is a .db save, removing linked .fwl file")
				os.Remove(f.FileDestinationPath)
				base := strings.TrimSuffix(f.FileDestinationPath, ".db")
				os.Remove(fmt.Sprintf("%s%s", base, ".fwl"))
			} else if strings.HasSuffix(f.Prefix, ".fwl") {
				log.Infof("file is a .fwl save, removing linked .db file")
				os.Remove(f.FileDestinationPath)
				base := strings.TrimSuffix(f.FileDestinationPath, ".fwl")
				os.Remove(fmt.Sprintf("%s%s", base, ".db"))
			} else {
				err := os.Remove(f.FileDestinationPath)
				if err != nil {
					return err
				}
			}
		}
	}

	log.Infof("current state of files in: %s", BACKUPS_DIR)
	files, err := f.ListFiles(BACKUPS_DIR, func(fileName string) bool {
		return true
	})
	if err != nil {
		return err
	}

	for _, file := range files {
		log.Infof("%s - %v", file.Name, file.Size())
	}

	log.Infof("current state of files in: %s", PLUGINS_DIR)
	files, err = f.ListFiles(PLUGINS_DIR, func(fileName string) bool {
		return true
	})
	if err != nil {
		return err
	}

	for _, file := range files {
		log.Infof("%s - %v", file.Name(), file.Size())
	}

	log.Infof("current state of files in: %s", CONFIG_DIR)
	files, err = f.ListFiles(CONFIG_DIR, func(fileName string) bool {
		return true
	})
	if err != nil {
		return err
	}

	for _, file := range files {
		log.Infof("%s - %v", file.Name(), file.Size())
	}

	return nil
}

// ListFiles List files in a given directory and adds files to a list which pass the given predicate function.
func (f *FileManager) ListFiles(dirPath string, predicate func(string) bool) ([]os.FileInfo, error) {
	dir, err := os.Open(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open directory: %w", err)
	}
	defer dir.Close()

	fileInfos, err := dir.Readdir(-1) // -1 means read all entries
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var files []os.FileInfo
	for _, fileInfo := range fileInfos {
		if !fileInfo.IsDir() && predicate(fileInfo.Name()) {
			files = append(files, fileInfo)
		}
	}

	return files, nil
}

// DirExists Checks for the presence of a directory on the (assumed) mounted PVC.
func (f *FileManager) DirExists(dir string) bool {
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
