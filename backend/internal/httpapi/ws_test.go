package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketReceivesFileCreatedEvent(t *testing.T) {
	cfg := testConfig(t)
	database := testDB(t)
	router := NewRouter(cfg, database)
	server := httptest.NewServer(router)
	defer server.Close()

	accessToken := registerAndLoginURL(t, server.URL)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/ws"
	dialer := *websocket.DefaultDialer
	dialer.Subprotocols = []string{"hopper", accessToken}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	uploadResp := uploadFileURL(t, server.URL, accessToken, "socket.txt", "socket content")
	if uploadResp.ID == "" {
		t.Fatalf("upload returned empty id: %+v", uploadResp)
	}

	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	var event struct {
		Type    string `json:"type"`
		Payload struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"payload"`
	}
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read websocket event: %v", err)
	}
	if event.Type != "file.created" || event.Payload.ID != uploadResp.ID || event.Payload.Name != "socket.txt" {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func registerAndLoginURL(t *testing.T, baseURL string) string {
	t.Helper()

	postJSONURL(t, baseURL+"/api/auth/register", map[string]string{
		"login":    "websocket",
		"password": "correct-password",
	}, "")

	var login tokenResponse
	postJSONURLInto(t, baseURL+"/api/auth/login", map[string]string{
		"login":    "websocket",
		"password": "correct-password",
	}, "", &login)
	if login.AccessToken == "" {
		t.Fatal("login did not return access token")
	}
	return login.AccessToken
}

func uploadFileURL(t *testing.T, baseURL, token, fileName, content string) struct {
	ID string `json:"id"`
} {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := io.Copy(part, strings.NewReader(content)); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/files/upload", &body)
	if err != nil {
		t.Fatalf("create upload request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload status = %d, body = %s", resp.StatusCode, raw)
	}

	var uploaded struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	return uploaded
}

func postJSONURL(t *testing.T, target string, body any, token string) {
	t.Helper()
	var ignored map[string]any
	postJSONURLInto(t, target, body, token, &ignored)
}

func postJSONURLInto(t *testing.T, target string, body any, token string, dst any) {
	t.Helper()

	var encoded bytes.Buffer
	if err := json.NewEncoder(&encoded).Encode(body); err != nil {
		t.Fatalf("encode json: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, target, &encoded)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post json: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("post %s status = %d, body = %s", target, resp.StatusCode, raw)
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
