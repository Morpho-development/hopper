package httpapi

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"hopper/backend/internal/config"
	"hopper/backend/internal/db"
)

func TestAuthFlow(t *testing.T) {
	database := testDB(t)
	router := NewRouter(testConfig(t), database)

	registerResp := doJSON(t, router, http.MethodPost, "/api/auth/register", map[string]string{
		"login":    "Test.User",
		"password": "correct-password",
	}, "")
	if registerResp.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", registerResp.Code, registerResp.Body.String())
	}

	var storedHash string
	if err := database.QueryRow("SELECT password_hash FROM users WHERE login = ?", "test.user").Scan(&storedHash); err != nil {
		t.Fatalf("select password hash: %v", err)
	}
	if storedHash == "correct-password" {
		t.Fatal("password was stored in plaintext")
	}

	loginResp := doJSON(t, router, http.MethodPost, "/api/auth/login", map[string]string{
		"login":    "test.user",
		"password": "correct-password",
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginResp.Code, loginResp.Body.String())
	}

	var login tokenResponse
	decodeResponse(t, loginResp, &login)
	loginCookie := authCookie(t, loginResp)
	if login.AccessToken == "" || loginCookie.Value == "" {
		t.Fatalf("login did not return tokens: %+v", login)
	}
	crossOriginRequest := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	crossOriginRequest.AddCookie(loginCookie)
	crossOriginRequest.Header.Set("Origin", "https://evil.example")
	crossOriginResponse := httptest.NewRecorder()
	router.ServeHTTP(crossOriginResponse, crossOriginRequest)
	if crossOriginResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-origin refresh status = %d", crossOriginResponse.Code)
	}

	unauthorizedResp := doJSON(t, router, http.MethodGet, "/api/auth/me", nil, "")
	if unauthorizedResp.Code != http.StatusUnauthorized {
		t.Fatalf("me without token status = %d", unauthorizedResp.Code)
	}

	meResp := doJSON(t, router, http.MethodGet, "/api/auth/me", nil, login.AccessToken)
	if meResp.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", meResp.Code, meResp.Body.String())
	}

	var me userResponse
	decodeResponse(t, meResp, &me)
	if me.User.Login != "test.user" {
		t.Fatalf("me login = %q", me.User.Login)
	}

	refreshResp := doJSONWithCookie(t, router, http.MethodPost, "/api/auth/refresh", nil, "", loginCookie)
	if refreshResp.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", refreshResp.Code, refreshResp.Body.String())
	}

	var refreshed tokenResponse
	decodeResponse(t, refreshResp, &refreshed)
	refreshedCookie := authCookie(t, refreshResp)
	if refreshed.AccessToken == "" || refreshedCookie.Value == "" {
		t.Fatalf("refresh did not return tokens: %+v", refreshed)
	}
	if refreshedCookie.Value == loginCookie.Value {
		t.Fatal("refresh token was not rotated")
	}

	reuseResp := doJSONWithCookie(t, router, http.MethodPost, "/api/auth/refresh", nil, "", loginCookie)
	if reuseResp.Code != http.StatusUnauthorized {
		t.Fatalf("old refresh token reuse status = %d", reuseResp.Code)
	}

	logoutResp := doJSONWithCookie(t, router, http.MethodPost, "/api/auth/logout", nil, "", refreshedCookie)
	if logoutResp.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", logoutResp.Code)
	}
	afterLogout := doJSONWithCookie(t, router, http.MethodPost, "/api/auth/refresh", nil, "", refreshedCookie)
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout status = %d", afterLogout.Code)
	}
}

func TestUpdateCredentialsRevokesOldRefreshTokens(t *testing.T) {
	database := testDB(t)
	router := NewRouter(testConfig(t), database)

	register := doJSON(t, router, http.MethodPost, "/api/auth/register", map[string]string{
		"login": "before", "password": "old-password",
	}, "")
	if register.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", register.Code, register.Body.String())
	}
	loginResponse := doJSON(t, router, http.MethodPost, "/api/auth/login", map[string]string{
		"login": "before", "password": "old-password",
	}, "")
	var oldTokens tokenResponse
	decodeResponse(t, loginResponse, &oldTokens)
	oldCookie := authCookie(t, loginResponse)

	wrongPassword := doJSON(t, router, http.MethodPatch, "/api/auth/me", map[string]any{
		"currentPassword": "wrong-password", "newLogin": "after",
	}, oldTokens.AccessToken)
	if wrongPassword.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password status = %d", wrongPassword.Code)
	}

	updatedResponse := doJSON(t, router, http.MethodPatch, "/api/auth/me", map[string]any{
		"currentPassword": "old-password",
		"newLogin":        "After",
		"newPassword":     "new-password",
	}, oldTokens.AccessToken)
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updatedResponse.Code, updatedResponse.Body.String())
	}
	var updated tokenResponse
	decodeResponse(t, updatedResponse, &updated)
	if updated.User.Login != "after" || updated.AccessToken == "" || authCookie(t, updatedResponse).Value == "" {
		t.Fatalf("unexpected updated session: %+v", updated)
	}
	oldAccess := doJSON(t, router, http.MethodGet, "/api/auth/me", nil, oldTokens.AccessToken)
	if oldAccess.Code != http.StatusUnauthorized {
		t.Fatalf("old access token status = %d", oldAccess.Code)
	}

	oldRefresh := doJSONWithCookie(t, router, http.MethodPost, "/api/auth/refresh", nil, "", oldCookie)
	if oldRefresh.Code != http.StatusUnauthorized {
		t.Fatalf("old refresh status = %d", oldRefresh.Code)
	}
	oldLogin := doJSON(t, router, http.MethodPost, "/api/auth/login", map[string]string{
		"login": "before", "password": "old-password",
	}, "")
	if oldLogin.Code != http.StatusUnauthorized {
		t.Fatalf("old credentials status = %d", oldLogin.Code)
	}
	newLogin := doJSON(t, router, http.MethodPost, "/api/auth/login", map[string]string{
		"login": "after", "password": "new-password",
	}, "")
	if newLogin.Code != http.StatusOK {
		t.Fatalf("new credentials status = %d, body = %s", newLogin.Code, newLogin.Body.String())
	}
}

type tokenResponse struct {
	AccessToken string `json:"accessToken"`
	ExpiresIn   int    `json:"expiresIn"`
	User        struct {
		ID    string `json:"id"`
		Login string `json:"login"`
	} `json:"user"`
}

type userResponse struct {
	User struct {
		ID    string `json:"id"`
		Login string `json:"login"`
	} `json:"user"`
}

func testConfig(t *testing.T) config.Config {
	t.Helper()

	return config.Config{
		AppDomain:              "localhost",
		AppEnv:                 "test",
		HTTPAddr:               "127.0.0.1:0",
		DatabasePath:           filepath.Join(t.TempDir(), "hopper.db"),
		StoragePath:            filepath.Join(t.TempDir(), "storage"),
		JWTAccessSecret:        "test-access-secret",
		AccessTokenTTL:         15 * time.Minute,
		RefreshTokenTTL:        time.Hour,
		RegistrationEnabled:    true,
		MaxUploadBytes:         1024 * 1024,
		InstanceQuotaBytes:     10 * 1024 * 1024,
		AuthRateLimitPerMinute: 20,
	}
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "hopper.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	return database
}

func doJSON(t *testing.T, handler http.Handler, method, path string, body any, accessToken string) *httptest.ResponseRecorder {
	return doJSONWithCookie(t, handler, method, path, body, accessToken, nil)
}

func doJSONWithCookie(t *testing.T, handler http.Handler, method, path string, body any, accessToken string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	var encoded bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&encoded).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}

	req := httptest.NewRequest(method, path, &encoded)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func authCookie(t *testing.T, recorder *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "hopper_refresh" {
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
				t.Fatalf("refresh cookie lacks security attributes: %+v", cookie)
			}
			return cookie
		}
	}
	t.Fatalf("response did not set refresh cookie: %s", recorder.Body.String())
	return nil
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder, dst any) {
	t.Helper()

	if err := json.NewDecoder(recorder.Body).Decode(dst); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
}
