package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	AppDomain              string
	AppEnv                 string
	HTTPAddr               string
	DatabasePath           string
	StoragePath            string
	JWTAccessSecret        string
	AccessTokenTTL         time.Duration
	RefreshTokenTTL        time.Duration
	RegistrationEnabled    bool
	FirstRunAdminLogin     string
	FirstRunAdminPassword  string
	MaxUploadBytes         int64
	InstanceQuotaBytes     int64
	AuthRateLimitPerMinute int
}

func Load() (Config, error) {
	cfg := Config{
		AppDomain:              getEnv("APP_DOMAIN", "localhost"),
		AppEnv:                 getEnv("APP_ENV", "development"),
		HTTPAddr:               getEnv("HTTP_ADDR", ":8080"),
		DatabasePath:           getEnv("DATABASE_PATH", "./hopper.db"),
		StoragePath:            getEnv("STORAGE_PATH", "./storage"),
		JWTAccessSecret:        os.Getenv("JWT_ACCESS_SECRET"),
		RegistrationEnabled:    getEnvBool("REGISTRATION_ENABLED", true),
		FirstRunAdminLogin:     getEnv("FIRST_RUN_ADMIN_LOGIN", "admin"),
		FirstRunAdminPassword:  os.Getenv("FIRST_RUN_ADMIN_PASSWORD"),
		MaxUploadBytes:         getEnvInt64("MAX_UPLOAD_BYTES", 100*1024*1024),
		InstanceQuotaBytes:     getEnvInt64("INSTANCE_QUOTA_BYTES", 10*1024*1024*1024),
		AuthRateLimitPerMinute: getEnvInt("AUTH_RATE_LIMIT_PER_MINUTE", 10),
	}

	var err error
	cfg.AccessTokenTTL, err = time.ParseDuration(getEnv("ACCESS_TOKEN_TTL", "15m"))
	if err != nil {
		return Config{}, fmt.Errorf("parse ACCESS_TOKEN_TTL: %w", err)
	}

	cfg.RefreshTokenTTL, err = time.ParseDuration(getEnv("REFRESH_TOKEN_TTL", "720h"))
	if err != nil {
		return Config{}, fmt.Errorf("parse REFRESH_TOKEN_TTL: %w", err)
	}

	if cfg.JWTAccessSecret == "" {
		return Config{}, errors.New("JWT_ACCESS_SECRET is required")
	}
	if len(cfg.JWTAccessSecret) < 32 {
		return Config{}, errors.New("JWT_ACCESS_SECRET must be at least 32 characters")
	}
	if cfg.MaxUploadBytes <= 0 {
		return Config{}, errors.New("MAX_UPLOAD_BYTES must be greater than zero")
	}
	if cfg.InstanceQuotaBytes <= 0 {
		return Config{}, errors.New("INSTANCE_QUOTA_BYTES must be greater than zero")
	}
	if cfg.AuthRateLimitPerMinute <= 0 {
		return Config{}, errors.New("AUTH_RATE_LIMIT_PER_MINUTE must be greater than zero")
	}
	if cfg.FirstRunAdminLogin == "" {
		return Config{}, errors.New("FIRST_RUN_ADMIN_LOGIN must not be empty")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvInt64(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}
