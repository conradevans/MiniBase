package integrationauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const tokenEntropyBytes = 32

var ErrInvalidToken = errors.New("invalid integration token")

func Ensure(path string) ([]byte, error) {
	token, err := Load(path)
	if err == nil {
		return token, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	parent := filepath.Dir(path)
	if err := ensureDirectory(parent); err != nil {
		return nil, err
	}
	random := make([]byte, tokenEntropyBytes)
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		return nil, fmt.Errorf("generate integration token: %w", err)
	}
	token = []byte(hex.EncodeToString(random))
	temporary, err := os.CreateTemp(parent, ".minideploy-token-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temporary integration token: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return nil, fmt.Errorf("restrict temporary integration token: %w", err)
	}
	if _, err := temporary.Write(append(append([]byte(nil), token...), '\n')); err != nil {
		temporary.Close()
		return nil, fmt.Errorf("write integration token: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return nil, fmt.Errorf("sync integration token: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return nil, fmt.Errorf("close integration token: %w", err)
	}
	if err := unix.Renameat2(unix.AT_FDCWD, temporaryPath, unix.AT_FDCWD, path, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return Load(path)
		}
		return nil, fmt.Errorf("install integration token: %w", err)
	}
	if err := syncDirectory(parent); err != nil {
		return nil, err
	}
	return append([]byte(nil), token...), nil
}

func Load(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("integration token path must not be empty")
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("resolve integration token path: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("integration token is not a regular file")
	}
	if info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("integration token permissions must be 0600")
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return nil, fmt.Errorf("read integration token: %w", err)
	}
	token := []byte(strings.TrimSpace(string(content)))
	if len(token) < 32 || strings.ContainsAny(string(token), " \t\r\n") {
		return nil, ErrInvalidToken
	}
	return token, nil
}

func Authorized(header string, expected []byte) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) || strings.Contains(header[len(prefix):], " ") {
		return false
	}
	supplied := []byte(header[len(prefix):])
	return len(supplied) == len(expected) && subtle.ConstantTimeCompare(supplied, expected) == 1
}

func ensureDirectory(path string) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("integration token parent is not a regular directory")
		}
	case os.IsNotExist(err):
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create integration token directory: %w", err)
		}
	default:
		return fmt.Errorf("inspect integration token directory: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("restrict integration token directory: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open integration token directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync integration token directory: %w", err)
	}
	return nil
}
