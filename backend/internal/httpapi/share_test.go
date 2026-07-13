package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPublicShareDownloadAndMaxDownloads(t *testing.T) {
	cfg := testConfig(t)
	database := testDB(t)
	router := NewRouter(cfg, database)
	token := registerAndLogin(t, router)

	uploadResp := uploadFile(t, router, token, "shared.txt", "public content")
	var uploaded struct {
		ID string `json:"id"`
	}
	decodeResponse(t, uploadResp, &uploaded)

	shareResp := doJSON(t, router, http.MethodPost, "/api/files/"+uploaded.ID+"/share", map[string]any{
		"expiresInSeconds": 3600,
		"maxDownloads":     1,
	}, token)
	if shareResp.Code != http.StatusCreated {
		t.Fatalf("share status = %d, body = %s", shareResp.Code, shareResp.Body.String())
	}

	var created struct {
		URL          string `json:"url"`
		ExpiresAt    string `json:"expiresAt"`
		MaxDownloads int    `json:"maxDownloads"`
	}
	decodeResponse(t, shareResp, &created)
	if created.URL == "" || created.MaxDownloads != 1 {
		t.Fatalf("unexpected share response: %+v", created)
	}

	publicToken := created.URL[strings.LastIndex(created.URL, "/")+1:]
	var storedHash string
	if err := database.QueryRow("SELECT token_hash FROM shares LIMIT 1").Scan(&storedHash); err != nil {
		t.Fatalf("select token hash: %v", err)
	}
	if storedHash == publicToken || strings.Contains(storedHash, publicToken) {
		t.Fatal("share token was stored in plaintext")
	}

	publicReq := newAuthRequest(http.MethodGet, "/s/"+publicToken, nil, "")
	publicResp := execute(router, publicReq)
	if publicResp.Code != http.StatusOK {
		t.Fatalf("public download status = %d, body = %s", publicResp.Code, publicResp.Body.String())
	}
	if publicResp.Body.String() != "public content" {
		t.Fatalf("public download body = %q", publicResp.Body.String())
	}

	secondReq := newAuthRequest(http.MethodGet, "/s/"+publicToken, nil, "")
	secondResp := execute(router, secondReq)
	if secondResp.Code != http.StatusNotFound {
		t.Fatalf("second public download status = %d, body = %s", secondResp.Code, secondResp.Body.String())
	}
}

func TestPublicShareExpired(t *testing.T) {
	cfg := testConfig(t)
	database := testDB(t)
	router := NewRouter(cfg, database)
	token := registerAndLogin(t, router)

	uploadResp := uploadFile(t, router, token, "expired.txt", "expired content")
	var uploaded struct {
		ID string `json:"id"`
	}
	decodeResponse(t, uploadResp, &uploaded)

	shareResp := doJSON(t, router, http.MethodPost, "/api/files/"+uploaded.ID+"/share", map[string]any{
		"expiresInSeconds": 1,
	}, token)
	var created struct {
		URL string `json:"url"`
	}
	decodeResponse(t, shareResp, &created)
	publicToken := created.URL[strings.LastIndex(created.URL, "/")+1:]

	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if _, err := database.Exec("UPDATE shares SET expires_at = ?", expired); err != nil {
		t.Fatalf("expire share: %v", err)
	}

	publicReq := newAuthRequest(http.MethodGet, "/s/"+publicToken, nil, "")
	publicResp := execute(router, publicReq)
	if publicResp.Code != http.StatusNotFound {
		t.Fatalf("expired public download status = %d, body = %s", publicResp.Code, publicResp.Body.String())
	}
}
