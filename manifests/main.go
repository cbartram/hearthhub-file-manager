package main

import (
	"archive/zip"
	"database/sql"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
	pluginsVolume = "/mnt/plugins"
	pluginsPath   = "/mnt/plugins/bepinex/plugins"
)

var db *sql.DB

func main() {
	var err error
	// Connect to PostgreSQL
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		log.Fatal("DATABASE_URL environment variable not set")
	}

	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	}
	defer db.Close()

	// Start processing plugins
	processPlugins()
}

func processPlugins() {
	for {
		plugins, err := getPendingPlugins()
		if err != nil {
			log.Printf("Error getting pending plugins: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		for _, plugin := range plugins {
			if err := installPlugin(plugin); err != nil {
				log.Printf("Error installing plugin %s: %v", plugin.Name, err)
				updatePluginStatus(plugin.ID, "failed")
				continue
			}
			updatePluginStatus(plugin.ID, "completed")
		}

		time.Sleep(10 * time.Second)
	}
}

func getPendingPlugins() ([]Plugin, error) {
	rows, err := db.Query(`
        SELECT id, user_id, name, url, status, created_at 
        FROM plugin_installations 
        WHERE status = 'pending'
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plugins []Plugin
	for rows.Next() {
		var p Plugin
		err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.URL, &p.Status, &p.CreatedAt)
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, p)
	}
	return plugins, nil
}

func installPlugin(plugin Plugin) error {
	// Download plugin
	resp, err := http.Get(plugin.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Create temp file
	tmpFile, err := os.CreateTemp("", "plugin-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	// Copy to temp file
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return err
	}
	tmpFile.Close()

	// Create plugins directory if it doesn't exist
	if err := os.MkdirAll(pluginsPath, 0755); err != nil {
		return err
	}

	// Unzip plugin
	if err := unzipFile(tmpFile.Name(), pluginsPath); err != nil {
		return err
	}

	return nil
}

func unzipFile(zipFile, dest string) error {
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
