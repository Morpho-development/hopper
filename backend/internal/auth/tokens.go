package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type TokenService struct {
	accessSecret []byte
	accessTTL    time.Duration
}

type AccessClaims struct {
	UserID      string
	Login       string
	AuthVersion int64
}

func NewTokenService(accessSecret string, accessTTL time.Duration) *TokenService {
	return &TokenService{
		accessSecret: []byte(accessSecret),
		accessTTL:    accessTTL,
	}
}

func (t *TokenService) NewAccessToken(user User) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":   user.ID,
		"login": user.Login,
		"ver":   user.AuthVersion,
		"iat":   now.Unix(),
		"exp":   now.Add(t.accessTTL).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(t.accessSecret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return signed, nil
}

func (t *TokenService) ParseAccessToken(raw string) (AccessClaims, error) {
	token, err := jwt.Parse(raw, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return t.accessSecret, nil
	})
	if err != nil {
		return AccessClaims{}, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return AccessClaims{}, errors.New("invalid claims")
	}

	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return AccessClaims{}, errors.New("missing subject")
	}

	login, _ := claims["login"].(string)
	version, exists := claims["ver"]
	if !exists {
		return AccessClaims{}, errors.New("missing auth version")
	}
	versionNumber, ok := version.(float64)
	if !ok || versionNumber < 1 {
		return AccessClaims{}, errors.New("invalid auth version")
	}
	return AccessClaims{UserID: sub, Login: login, AuthVersion: int64(versionNumber)}, nil
}
