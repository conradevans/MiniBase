package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/conradevans/MiniBase/internal/ids"
	"golang.org/x/sys/unix"
)

const passwordEntropyBytes = 32

var (
	ErrCredentialExists  = errors.New("database credential already exists")
	ErrCredentialMissing = errors.New("database credential is missing")
)

type Store struct {
	root   string
	random io.Reader
}

func New(root string) (*Store, error) {
	if root == "" {
		return nil, fmt.Errorf("database secret root must not be empty")
	}
	absoluteRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, fmt.Errorf("resolve database secret root: %w", err)
	}
	return &Store{root: absoluteRoot, random: rand.Reader}, nil
}

func (s *Store) Create(databaseID string) (string, error) {
	if !ids.ValidDatabaseID(databaseID) {
		return "", fmt.Errorf("invalid database resource ID")
	}
	databaseDirectory, passwordPath, err := s.prepareDirectory(databaseID)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(passwordPath); err == nil {
		return "", ErrCredentialExists
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect database credential: %w", err)
	}

	randomBytes := make([]byte, passwordEntropyBytes)
	if _, err := io.ReadFull(s.random, randomBytes); err != nil {
		return "", fmt.Errorf("generate database credential: %w", err)
	}
	password := hex.EncodeToString(randomBytes)

	temporaryFile, err := os.CreateTemp(databaseDirectory, ".password.tmp-")
	if err != nil {
		return "", fmt.Errorf("create temporary database credential: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	cleanupTemporary := true
	defer func() {
		if cleanupTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporaryFile.Chmod(0o600); err != nil {
		_ = temporaryFile.Close()
		return "", fmt.Errorf("restrict temporary database credential: %w", err)
	}
	if _, err := temporaryFile.WriteString(password + "\n"); err != nil {
		_ = temporaryFile.Close()
		return "", fmt.Errorf("write temporary database credential: %w", err)
	}
	if err := temporaryFile.Sync(); err != nil {
		_ = temporaryFile.Close()
		return "", fmt.Errorf("sync temporary database credential: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return "", fmt.Errorf("close temporary database credential: %w", err)
	}

	if err := unix.Renameat2(unix.AT_FDCWD, temporaryPath, unix.AT_FDCWD, passwordPath, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return "", ErrCredentialExists
		}
		return "", fmt.Errorf("install database credential atomically: %w", err)
	}
	cleanupTemporary = false

	if err := syncDirectory(databaseDirectory); err != nil {
		return "", err
	}
	return password, nil
}

func (s *Store) Exists(databaseID string) (bool, error) {
	if !ids.ValidDatabaseID(databaseID) {
		return false, fmt.Errorf("invalid database resource ID")
	}
	passwordPath := filepath.Join(s.root, databaseID, "password")
	info, err := os.Lstat(passwordPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect database credential: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("database credential is not a regular file")
	}
	if info.Mode().Perm() != 0o600 && info.Mode().Perm() != 0o400 {
		return false, fmt.Errorf("database credential permissions are not owner-only")
	}
	return true, nil
}

func (s *Store) Delete(databaseID string) error {
	if !ids.ValidDatabaseID(databaseID) {
		return fmt.Errorf("invalid database resource ID")
	}
	databaseDirectory := filepath.Join(s.root, databaseID)
	passwordPath := filepath.Join(databaseDirectory, "password")

	if err := os.Remove(passwordPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove database credential: %w", err)
	}
	if err := os.Remove(databaseDirectory); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove database credential directory: %w", err)
	}
	if err := syncDirectory(s.root); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) prepareDirectory(databaseID string) (string, string, error) {
	if err := ensureSecureDirectory(s.root); err != nil {
		return "", "", err
	}
	databaseDirectory := filepath.Join(s.root, databaseID)
	if err := ensureSecureDirectory(databaseDirectory); err != nil {
		return "", "", err
	}
	return databaseDirectory, filepath.Join(databaseDirectory, "password"), nil
}

func ensureSecureDirectory(path string) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("database secret path is not a regular directory")
		}
	case os.IsNotExist(err):
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create database secret directory: %w", err)
		}
	default:
		return fmt.Errorf("inspect database secret directory: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("restrict database secret directory: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open database secret directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync database secret directory: %w", err)
	}
	return nil
}
