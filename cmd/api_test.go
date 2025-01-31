package cmd

import (
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMakeHearthHubClient(t *testing.T) {
	c := MakeHearthHubClient("foo")
	assert.NotNil(t, c)
	assert.Equal(t, "foo", c.BaseUrl)
}

func TestScaleDeployment(t *testing.T) {
	tests := []struct {
		name          string
		scale         int
		statusCode    int
		responseBody  string
		expectedError string
	}{
		{
			name:          "Scale to 1 - Success",
			scale:         1,
			statusCode:    200,
			responseBody:  `{"status": "success"}`,
			expectedError: "",
		},
		{
			name:          "Scale to 0 - Success",
			scale:         0,
			statusCode:    200,
			responseBody:  `{"status": "success"}`,
			expectedError: "",
		},
		{
			name:          "Scale to 1 - Server Already Running",
			scale:         1,
			statusCode:    400,
			responseBody:  `{"error": "server already running"}`,
			expectedError: "",
		},
		{
			name:          "Scale to 0 - No Server to Terminate",
			scale:         0,
			statusCode:    400,
			responseBody:  `{"error": "no server to terminate"}`,
			expectedError: "",
		},
		{
			name:          "Scale to 1 - Unexpected Error",
			scale:         1,
			statusCode:    500,
			responseBody:  `{"error": "internal server error"}`,
			expectedError: "failed to scale replica to: 1, status code: 500, body: {\"error\": \"internal server error\"}",
		},
		{
			name:          "Scale to 0 - Unexpected Error",
			scale:         0,
			statusCode:    500,
			responseBody:  `{"error": "internal server error"}`,
			expectedError: "failed to scale replica to: 0, status code: 500, body: {\"error\": \"internal server error\"}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			// Create a HearthHubClient with the mock server's URL
			client := &HearthHubClient{
				BaseUrl: server.URL,
			}

			// Create a FileManager with dummy credentials
			fileManager := &FileManager{
				DiscordId:    "test-discord-id",
				RefreshToken: "test-refresh-token",
			}

			// Call the ScaleDeployment function
			err := client.ScaleDeployment(fileManager, tt.scale)

			// Check the error
			if tt.expectedError == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil || err.Error() != tt.expectedError {
					t.Errorf("expected error %v, got %v", tt.expectedError, err)
				}
			}
		})
	}
}
