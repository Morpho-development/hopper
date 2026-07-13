package files

import (
	"path/filepath"
	"runtime"
	"strings"
)

func CleanDisplayName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrInvalidPath
	}
	if name != filepath.Base(name) {
		return "", ErrInvalidPath
	}
	if strings.Contains(name, "/") || strings.Contains(name, `\`) {
		return "", ErrInvalidPath
	}
	if strings.Contains(name, "..") {
		return "", ErrInvalidPath
	}
	if runtime.GOOS == "windows" && strings.Contains(name, ":") {
		return "", ErrInvalidPath
	}
	return name, nil
}

func SafeJoin(root, storageName string) (string, error) {
	if root == "" || storageName == "" {
		return "", ErrInvalidPath
	}
	if filepath.IsAbs(storageName) || storageName != filepath.Base(storageName) {
		return "", ErrInvalidPath
	}
	if strings.Contains(storageName, "/") || strings.Contains(storageName, `\`) || strings.Contains(storageName, "..") {
		return "", ErrInvalidPath
	}

	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}

	fullPath := filepath.Join(cleanRoot, storageName)
	rel, err := filepath.Rel(cleanRoot, fullPath)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", ErrInvalidPath
	}

	return fullPath, nil
}
