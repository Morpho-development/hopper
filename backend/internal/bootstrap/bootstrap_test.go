package bootstrap

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"hopper/backend/internal/auth"
	"hopper/backend/internal/config"
	"hopper/backend/internal/db"
)

func TestEnsureFirstUserCreatesGeneratedPasswordOnce(t *testing.T) {
	database := openTestDB(t)
	cfg := testConfig(t)

	result, err := EnsureFirstUser(context.Background(), cfg, database)
	if err != nil {
		t.Fatalf("EnsureFirstUser returned error: %v", err)
	}
	if !result.Created {
		t.Fatal("expected first user to be created")
	}
	if result.Login != cfg.FirstRunAdminLogin {
		t.Fatalf("login = %q", result.Login)
	}
	if result.Password == "" || !result.PasswordGenerated {
		t.Fatalf("expected generated password, got %+v", result)
	}

	store := auth.NewStore(database)
	if _, err := store.Authenticate(context.Background(), result.Login, result.Password); err != nil {
		t.Fatalf("generated credentials do not authenticate: %v", err)
	}

	second, err := EnsureFirstUser(context.Background(), cfg, database)
	if err != nil {
		t.Fatalf("second EnsureFirstUser returned error: %v", err)
	}
	if second.Created {
		t.Fatalf("second bootstrap should not create another user: %+v", second)
	}
}

func TestEnsureFirstUserUsesConfiguredPassword(t *testing.T) {
	database := openTestDB(t)
	cfg := testConfig(t)
	cfg.FirstRunAdminPassword = "configured-password"

	result, err := EnsureFirstUser(context.Background(), cfg, database)
	if err != nil {
		t.Fatalf("EnsureFirstUser returned error: %v", err)
	}
	if !result.Created || result.PasswordGenerated {
		t.Fatalf("expected configured password to be used: %+v", result)
	}

	store := auth.NewStore(database)
	if _, err := store.Authenticate(context.Background(), result.Login, "configured-password"); err != nil {
		t.Fatalf("configured credentials do not authenticate: %v", err)
	}
}

func TestEnsureFirstUserRequiresProductionPassword(t *testing.T) {
	database := openTestDB(t)
	cfg := testConfig(t)
	cfg.AppEnv = "production"
	cfg.FirstRunAdminPassword = ""

	if _, err := EnsureFirstUser(context.Background(), cfg, database); err == nil {
		t.Fatal("expected empty production password to be rejected")
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "hopper.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	return database
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
		FirstRunAdminLogin:     "admin",
		MaxUploadBytes:         1024 * 1024,
		InstanceQuotaBytes:     10 * 1024 * 1024,
		AuthRateLimitPerMinute: 20,
	}
}
