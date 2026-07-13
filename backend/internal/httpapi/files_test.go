package httpapi

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileUploadDownloadDeleteFlow(t *testing.T) {
	cfg := testConfig(t)
	database := testDB(t)
	router := NewRouter(cfg, database)
	token := registerAndLogin(t, router)

	uploadResp := uploadFile(t, router, token, "hello.txt", "hello from hopper")
	if uploadResp.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", uploadResp.Code, uploadResp.Body.String())
	}

	var uploaded struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		MimeType  string `json:"mimeType"`
		SizeBytes int64  `json:"sizeBytes"`
	}
	decodeResponse(t, uploadResp, &uploaded)
	if uploaded.ID == "" || uploaded.Name != "hello.txt" || uploaded.SizeBytes != int64(len("hello from hopper")) {
		t.Fatalf("unexpected upload response: %+v", uploaded)
	}

	var storageName string
	if err := database.QueryRow("SELECT storage_name FROM files WHERE id = ?", uploaded.ID).Scan(&storageName); err != nil {
		t.Fatalf("select storage name: %v", err)
	}
	if storageName == "hello.txt" || strings.Contains(storageName, "hello") {
		t.Fatalf("storage name used original name: %q", storageName)
	}
	if _, err := os.Stat(filepath.Join(cfg.StoragePath, storageName)); err != nil {
		t.Fatalf("stored file missing: %v", err)
	}

	downloadReq := newAuthRequest(http.MethodGet, "/api/files/"+uploaded.ID, nil, token)
	downloadResp := execute(router, downloadReq)
	if downloadResp.Code != http.StatusOK {
		t.Fatalf("download status = %d, body = %s", downloadResp.Code, downloadResp.Body.String())
	}
	if body := downloadResp.Body.String(); body != "hello from hopper" {
		t.Fatalf("download body = %q", body)
	}
	if disposition := downloadResp.Header().Get("Content-Disposition"); !strings.Contains(disposition, "hello.txt") {
		t.Fatalf("content disposition = %q", disposition)
	}

	deleteReq := newAuthRequest(http.MethodDelete, "/api/files/"+uploaded.ID, nil, token)
	deleteResp := execute(router, deleteReq)
	if deleteResp.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", deleteResp.Code, deleteResp.Body.String())
	}
	if _, err := os.Stat(filepath.Join(cfg.StoragePath, storageName)); !os.IsNotExist(err) {
		t.Fatalf("stored file still exists or stat failed unexpectedly: %v", err)
	}

	downloadAfterDeleteReq := newAuthRequest(http.MethodGet, "/api/files/"+uploaded.ID, nil, token)
	downloadAfterDeleteResp := execute(router, downloadAfterDeleteReq)
	if downloadAfterDeleteResp.Code != http.StatusNotFound {
		t.Fatalf("download after delete status = %d", downloadAfterDeleteResp.Code)
	}
}

func TestUploadRejectsTraversalFileName(t *testing.T) {
	cfg := testConfig(t)
	database := testDB(t)
	router := NewRouter(cfg, database)
	token := registerAndLogin(t, router)

	resp := uploadFile(t, router, token, "../secret.txt", "nope")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("upload traversal status = %d, body = %s", resp.Code, resp.Body.String())
	}
}

func TestUploadEnforcesMaxFileSize(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxUploadBytes = 4
	database := testDB(t)
	router := NewRouter(cfg, database)
	token := registerAndLogin(t, router)

	resp := uploadFile(t, router, token, "big.txt", "12345")
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("upload too large status = %d, body = %s", resp.Code, resp.Body.String())
	}
}

func TestFileListFoldersRenameAndMove(t *testing.T) {
	cfg := testConfig(t)
	database := testDB(t)
	router := NewRouter(cfg, database)
	token := registerAndLogin(t, router)

	folderResp := doJSON(t, router, http.MethodPost, "/api/folders/", map[string]any{
		"name": "Docs",
	}, token)
	if folderResp.Code != http.StatusCreated {
		t.Fatalf("create folder status = %d, body = %s", folderResp.Code, folderResp.Body.String())
	}

	var folder struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	decodeResponse(t, folderResp, &folder)
	if folder.ID == "" || folder.Name != "Docs" {
		t.Fatalf("unexpected folder response: %+v", folder)
	}

	uploadResp := uploadFile(t, router, token, "draft.txt", "draft")
	if uploadResp.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", uploadResp.Code, uploadResp.Body.String())
	}

	var uploaded struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	decodeResponse(t, uploadResp, &uploaded)

	rootListReq := newAuthRequest(http.MethodGet, "/api/files/?page=1&pageSize=20", nil, token)
	rootListResp := execute(router, rootListReq)
	if rootListResp.Code != http.StatusOK {
		t.Fatalf("root list status = %d, body = %s", rootListResp.Code, rootListResp.Body.String())
	}

	var rootList struct {
		Items []testListItem `json:"items"`
		Total int            `json:"total"`
	}
	decodeResponse(t, rootListResp, &rootList)
	if rootList.Total != 2 {
		t.Fatalf("root list total = %d, items = %+v", rootList.Total, rootList.Items)
	}
	if !containsListItem(rootList.Items, folder.ID, "folder", "Docs") {
		t.Fatalf("root list missing folder: %+v", rootList.Items)
	}
	if !containsListItem(rootList.Items, uploaded.ID, "file", "draft.txt") {
		t.Fatalf("root list missing file: %+v", rootList.Items)
	}

	patchResp := doJSON(t, router, http.MethodPatch, "/api/files/"+uploaded.ID, map[string]any{
		"name":     "final.txt",
		"folderId": folder.ID,
	}, token)
	if patchResp.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", patchResp.Code, patchResp.Body.String())
	}

	var patched struct {
		ID       string  `json:"id"`
		Name     string  `json:"name"`
		FolderID *string `json:"folderId"`
	}
	decodeResponse(t, patchResp, &patched)
	if patched.Name != "final.txt" || patched.FolderID == nil || *patched.FolderID != folder.ID {
		t.Fatalf("unexpected patch response: %+v", patched)
	}

	folderListReq := newAuthRequest(http.MethodGet, "/api/files/?folderId="+folder.ID, nil, token)
	folderListResp := execute(router, folderListReq)
	if folderListResp.Code != http.StatusOK {
		t.Fatalf("folder list status = %d, body = %s", folderListResp.Code, folderListResp.Body.String())
	}

	var folderList struct {
		Items []testListItem `json:"items"`
		Total int            `json:"total"`
	}
	decodeResponse(t, folderListResp, &folderList)
	if folderList.Total != 1 || !containsListItem(folderList.Items, uploaded.ID, "file", "final.txt") {
		t.Fatalf("folder list mismatch: %+v", folderList)
	}
}

func TestFileListIncludesStorageUsage(t *testing.T) {
	database := testDB(t)
	cfg := testConfig(t)
	cfg.InstanceQuotaBytes = 1024
	router := NewRouter(cfg, database)
	token := registerAndLogin(t, router)

	upload := uploadFile(t, router, token, "usage.txt", "12345")
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", upload.Code, upload.Body.String())
	}

	response := doJSON(t, router, http.MethodGet, "/api/files/", nil, token)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", response.Code, response.Body.String())
	}

	var body struct {
		Storage struct {
			UsedBytes      int64 `json:"usedBytes"`
			QuotaBytes     int64 `json:"quotaBytes"`
			AvailableBytes int64 `json:"availableBytes"`
		} `json:"storage"`
	}
	decodeResponse(t, response, &body)
	if body.Storage.UsedBytes != 5 || body.Storage.QuotaBytes != 1024 || body.Storage.AvailableBytes != 1019 {
		t.Fatalf("unexpected storage status: %+v", body.Storage)
	}
}

func TestPurgeRequiresPasswordAndConfirmation(t *testing.T) {
	cfg := testConfig(t)
	database := testDB(t)
	router := NewRouter(cfg, database)
	token := registerAndLogin(t, router)

	upload := uploadFile(t, router, token, "purge.txt", "purge content")
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status = %d", upload.Code)
	}
	var storageName string
	if err := database.QueryRow("SELECT storage_name FROM files LIMIT 1").Scan(&storageName); err != nil {
		t.Fatalf("select storage name: %v", err)
	}

	wrong := doJSON(t, router, http.MethodDelete, "/api/files/", map[string]string{
		"currentPassword": "wrong-password",
		"confirmation":    "DELETE ALL FILES",
	}, token)
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d", wrong.Code)
	}

	purge := doJSON(t, router, http.MethodDelete, "/api/files/", map[string]string{
		"currentPassword": "correct-password",
		"confirmation":    "DELETE ALL FILES",
	}, token)
	if purge.Code != http.StatusOK {
		t.Fatalf("purge status = %d, body = %s", purge.Code, purge.Body.String())
	}
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM files").Scan(&count); err != nil || count != 0 {
		t.Fatalf("files remain after purge: count=%d err=%v", count, err)
	}
	if _, err := os.Stat(filepath.Join(cfg.StoragePath, storageName)); !os.IsNotExist(err) {
		t.Fatalf("physical file remains after purge: %v", err)
	}
}

func TestPreviewAllowsImageAndRejectsHTML(t *testing.T) {
	router := NewRouter(testConfig(t), testDB(t))
	token := registerAndLogin(t, router)

	imageUpload := uploadFile(t, router, token, "pixel.png", "\x89PNG\r\n\x1a\npreview")
	var image struct {
		ID string `json:"id"`
	}
	decodeResponse(t, imageUpload, &image)
	imagePreview := httptest.NewRecorder()
	router.ServeHTTP(imagePreview, newAuthRequest(http.MethodGet, "/api/files/"+image.ID+"/preview", nil, token))
	if imagePreview.Code != http.StatusOK || imagePreview.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("image preview status=%d headers=%v", imagePreview.Code, imagePreview.Header())
	}
	if !strings.HasPrefix(imagePreview.Header().Get("Content-Disposition"), "inline") {
		t.Fatalf("image preview disposition = %q", imagePreview.Header().Get("Content-Disposition"))
	}

	htmlUpload := uploadFile(t, router, token, "page.html", "<!doctype html><script>alert(1)</script>")
	var html struct {
		ID string `json:"id"`
	}
	decodeResponse(t, htmlUpload, &html)
	htmlPreview := httptest.NewRecorder()
	router.ServeHTTP(htmlPreview, newAuthRequest(http.MethodGet, "/api/files/"+html.ID+"/preview", nil, token))
	if htmlPreview.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("HTML preview status = %d, body = %s", htmlPreview.Code, htmlPreview.Body.String())
	}
}

func TestFileMoveRejectsForeignFolder(t *testing.T) {
	cfg := testConfig(t)
	database := testDB(t)
	router := NewRouter(cfg, database)
	firstToken := registerAndLogin(t, router)

	secondRegister := doJSON(t, router, http.MethodPost, "/api/auth/register", map[string]string{
		"login":    "second",
		"password": "correct-password",
	}, "")
	if secondRegister.Code != http.StatusCreated {
		t.Fatalf("second register status = %d, body = %s", secondRegister.Code, secondRegister.Body.String())
	}
	secondLogin := doJSON(t, router, http.MethodPost, "/api/auth/login", map[string]string{
		"login":    "second",
		"password": "correct-password",
	}, "")
	var second tokenResponse
	decodeResponse(t, secondLogin, &second)

	foreignFolderResp := doJSON(t, router, http.MethodPost, "/api/folders/", map[string]any{"name": "Private"}, second.AccessToken)
	var foreignFolder struct {
		ID string `json:"id"`
	}
	decodeResponse(t, foreignFolderResp, &foreignFolder)

	uploadResp := uploadFile(t, router, firstToken, "mine.txt", "mine")
	var uploaded struct {
		ID string `json:"id"`
	}
	decodeResponse(t, uploadResp, &uploaded)

	patchResp := doJSON(t, router, http.MethodPatch, "/api/files/"+uploaded.ID, map[string]any{
		"folderId": foreignFolder.ID,
	}, firstToken)
	if patchResp.Code != http.StatusNotFound {
		t.Fatalf("move to foreign folder status = %d, body = %s", patchResp.Code, patchResp.Body.String())
	}
}

func registerAndLogin(t *testing.T, router http.Handler) string {
	t.Helper()

	registerResp := doJSON(t, router, http.MethodPost, "/api/auth/register", map[string]string{
		"login":    "files",
		"password": "correct-password",
	}, "")
	if registerResp.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", registerResp.Code, registerResp.Body.String())
	}

	loginResp := doJSON(t, router, http.MethodPost, "/api/auth/login", map[string]string{
		"login":    "files",
		"password": "correct-password",
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginResp.Code, loginResp.Body.String())
	}

	var login tokenResponse
	decodeResponse(t, loginResp, &login)
	return login.AccessToken
}

func uploadFile(t *testing.T, router http.Handler, token, fileName, content string) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+fileName+`"`)
	header.Set("Content-Type", "application/octet-stream")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := io.Copy(part, strings.NewReader(content)); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := newAuthRequest(http.MethodPost, "/api/files/upload", &body, token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return execute(router, req)
}

func newAuthRequest(method, path string, body io.Reader, token string) *http.Request {
	req := httptest.NewRequest(method, path, body)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func execute(handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

type testListItem struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

func containsListItem(items []testListItem, id, itemType, name string) bool {
	for _, item := range items {
		if item.ID == id && item.Type == itemType && item.Name == name {
			return true
		}
	}
	return false
}
