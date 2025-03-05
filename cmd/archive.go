package cmd

import (
	"archive/zip"
	log "github.com/sirupsen/logrus"
	"io"
	"os"
	"path/filepath"
)

type Archive struct {
	ZipFilePath string
	Destination string
}

// RemoveFilesFromZip Removes all the files that are present in a zip file from the destination as well as the zip file itself.
// This function reads the zip file to determine which files to delete and is used for mod uninstallation.
func (a *Archive) RemoveFilesFromZip() error {
	zipReader, err := zip.OpenReader(a.ZipFilePath)
	if err != nil {
		log.Infof("failed to open ZIP file: %v", err)
	}
	defer zipReader.Close()

	// Iterate over the files in the ZIP and remove them from the PVC
	for _, f := range zipReader.File {
		filePath := filepath.Join(a.Destination, f.Name)
		log.Infof("removing file %s", filePath)
		if err := os.Remove(filePath); err != nil {
			if os.IsNotExist(err) {
				log.Infof("file %s does not exist, skipping...", filePath)
				continue
			}
			log.Errorf("Failed to remove file %s: %v", filePath, err)
		}
	}

	if err := os.Remove(a.ZipFilePath); err != nil {
		log.Fatalf("failed to remove temporary ZIP file: %v", err)
	}
	return nil
}

// UnzipFile Unpacks an archived zip file to the specified path in the Archive class. Note:
// it is very important that the zip file is NOT cleaned up after unpack. Zip file names are used by the frontend
// as the source of truth for a mod. If the .dll files in the zip for the mod name don't match the zip file name
// then there are problems identifying which mods are actually installed. Therefore, leave the zip file alone after it's
// been downloaded!! Future downloads will just overwrite it so no big deal.
func (a *Archive) UnzipFile() error {
	reader, err := zip.OpenReader(a.ZipFilePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		path := filepath.Join(a.Destination, file.Name)

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
