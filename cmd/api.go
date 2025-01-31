package cmd

import (
	"bytes"
	"fmt"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"strings"
)

type HearthHubClient struct {
	BaseUrl string
}

func MakeHearthHubClient(baseUrl string) *HearthHubClient {
	return &HearthHubClient{
		BaseUrl: baseUrl,
	}
}

// ScaleDeployment Scales a Kubernetes Valheim Dedicated Server Deployment to either 1 or 0.
func (h *HearthHubClient) ScaleDeployment(fileManager *FileManager, scale int) error {
	method := "PUT"
	url := fmt.Sprintf("%s/api/v1/server/scale", h.BaseUrl)

	client := &http.Client{}
	req, err := http.NewRequest(method, url, bytes.NewReader([]byte(fmt.Sprintf(`{"replicas": %v}`, scale))))

	if err != nil {
		return err
	}
	req.SetBasicAuth(fileManager.DiscordId, fileManager.RefreshToken)

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
	log.Infof("response: PUT %s/api/v1/server/scale: %v response: %s", h.BaseUrl, scale, bodyString)

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
