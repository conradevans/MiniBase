package backups

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/conradevans/MiniBase/internal/ids"
)

var (
	ErrArchiveExists   = errors.New("backup archive already exists")
	ErrArchiveNotFound = errors.New("backup archive not found")
	ErrArchiveInvalid  = errors.New("backup archive is invalid")
)

type ArchiveReader struct {
	io.ReadCloser
	SizeBytes int64
}

type FileStore struct {
	root string
}

func NewFileStore(rootPath string) (*FileStore, error) {
	if rootPath == "" {
		return nil, fmt.Errorf("backup root must not be empty")
	}
	absoluteRoot, err := filepath.Abs(filepath.Clean(rootPath))
	if err != nil {
		return nil, fmt.Errorf("resolve backup root: %w", err)
	}
	store := &FileStore{root: absoluteRoot}
	if err := store.prepareRoot(); err != nil {
		return nil, err
	}
	return store, nil
}

func (store *FileStore) Create(
	databaseID string,
	backupID string,
	dump func(io.Writer) error,
	verify func(io.Reader) error,
) (sizeBytes int64, returnErr error) {
	if !ids.ValidDatabaseID(databaseID) || !ids.ValidBackupID(backupID) || dump == nil || verify == nil {
		return 0, ErrArchiveInvalid
	}
	root, err := store.openRoot()
	if err != nil {
		return 0, err
	}
	defer root.Close()

	if err := ensureDatabaseDirectory(root, databaseID); err != nil {
		return 0, err
	}
	temporaryName := temporaryArchiveName(databaseID, backupID)
	finalName := finalArchiveName(databaseID, backupID)

	if _, err := root.Lstat(finalName); err == nil {
		return 0, ErrArchiveExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("inspect final backup archive: %w", err)
	}
	if err := root.Remove(temporaryName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("remove stale partial backup archive: %w", err)
	}

	file, err := root.OpenFile(temporaryName, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return 0, ErrArchiveExists
		}
		return 0, fmt.Errorf("create partial backup archive: %w", err)
	}
	temporaryExists := true
	finalExists := false
	defer func() {
		if closeErr := file.Close(); returnErr == nil && closeErr != nil {
			returnErr = fmt.Errorf("close backup archive: %w", closeErr)
		}
		if returnErr != nil {
			if temporaryExists {
				_ = root.Remove(temporaryName)
			}
			if finalExists {
				_ = root.Remove(finalName)
			}
		}
	}()

	if err := file.Chmod(0o600); err != nil {
		return 0, fmt.Errorf("restrict partial backup archive: %w", err)
	}
	if err := dump(file); err != nil {
		return 0, fmt.Errorf("write backup archive: %w", err)
	}
	if err := file.Sync(); err != nil {
		return 0, fmt.Errorf("sync partial backup archive: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("inspect partial backup archive: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return 0, ErrArchiveInvalid
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, fmt.Errorf("rewind partial backup archive: %w", err)
	}
	if err := verify(file); err != nil {
		return 0, fmt.Errorf("verify backup archive: %w", err)
	}

	if err := root.Link(temporaryName, finalName); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return 0, ErrArchiveExists
		}
		return 0, fmt.Errorf("install backup archive without overwrite: %w", err)
	}
	finalExists = true
	if err := root.Remove(temporaryName); err != nil {
		return 0, fmt.Errorf("remove installed backup temporary link: %w", err)
	}
	temporaryExists = false
	if err := syncRootDirectory(root, databaseID); err != nil {
		return 0, err
	}

	finalInfo, err := root.Stat(finalName)
	if err != nil {
		return 0, fmt.Errorf("inspect installed backup archive: %w", err)
	}
	if !finalInfo.Mode().IsRegular() || finalInfo.Mode().Perm() != 0o600 || finalInfo.Size() <= 0 {
		return 0, ErrArchiveInvalid
	}
	return finalInfo.Size(), nil
}

func (store *FileStore) Open(databaseID, backupID string) (*ArchiveReader, error) {
	if !ids.ValidDatabaseID(databaseID) || !ids.ValidBackupID(backupID) {
		return nil, ErrArchiveInvalid
	}
	root, err := store.openRoot()
	if err != nil {
		return nil, err
	}
	archiveName := finalArchiveName(databaseID, backupID)
	linkInfo, err := root.Lstat(archiveName)
	if err != nil {
		root.Close()
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrArchiveNotFound
		}
		return nil, fmt.Errorf("inspect backup archive link: %w", err)
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 || !linkInfo.Mode().IsRegular() {
		root.Close()
		return nil, ErrArchiveInvalid
	}
	file, err := root.Open(archiveName)
	if err != nil {
		root.Close()
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrArchiveNotFound
		}
		return nil, fmt.Errorf("open backup archive: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		root.Close()
		return nil, fmt.Errorf("inspect backup archive: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() <= 0 {
		file.Close()
		root.Close()
		return nil, ErrArchiveInvalid
	}
	return &ArchiveReader{
		ReadCloser: &rootedArchiveReader{File: file, root: root},
		SizeBytes:  info.Size(),
	}, nil
}

func (store *FileStore) Delete(databaseID, backupID string) error {
	if !ids.ValidDatabaseID(databaseID) || !ids.ValidBackupID(backupID) {
		return ErrArchiveInvalid
	}
	root, err := store.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()

	if err := root.Remove(finalArchiveName(databaseID, backupID)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrArchiveNotFound
		}
		return fmt.Errorf("remove backup archive: %w", err)
	}
	if err := root.Remove(databaseID); err != nil && !errors.Is(err, fs.ErrNotExist) && !isDirectoryNotEmpty(err) {
		return fmt.Errorf("remove empty backup database directory: %w", err)
	}
	return syncRootDirectory(root, ".")
}

func (store *FileStore) RemovePartial(databaseID, backupID string) error {
	if !ids.ValidDatabaseID(databaseID) || !ids.ValidBackupID(backupID) {
		return ErrArchiveInvalid
	}
	root, err := store.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(temporaryArchiveName(databaseID, backupID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove partial backup archive: %w", err)
	}
	return nil
}

func (store *FileStore) prepareRoot() error {
	info, err := os.Lstat(store.root)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("backup root is not a regular directory")
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(store.root, 0o700); err != nil {
			return fmt.Errorf("create backup root: %w", err)
		}
	default:
		return fmt.Errorf("inspect backup root: %w", err)
	}
	if err := os.Chmod(store.root, 0o700); err != nil {
		return fmt.Errorf("restrict backup root: %w", err)
	}
	return nil
}

func (store *FileStore) openRoot() (*os.Root, error) {
	if err := store.prepareRoot(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return nil, fmt.Errorf("open backup root: %w", err)
	}
	return root, nil
}

func ensureDatabaseDirectory(root *os.Root, databaseID string) error {
	err := root.Mkdir(databaseID, 0o700)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("create backup database directory: %w", err)
	}
	info, err := root.Lstat(databaseID)
	if err != nil {
		return fmt.Errorf("inspect backup database directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("backup database path is not a regular directory")
	}
	if err := root.Chmod(databaseID, 0o700); err != nil {
		return fmt.Errorf("restrict backup database directory: %w", err)
	}
	return nil
}

func finalArchiveName(databaseID, backupID string) string {
	return databaseID + "/" + backupID + ".dump"
}

func temporaryArchiveName(databaseID, backupID string) string {
	return databaseID + "/." + backupID + ".tmp"
}

func syncRootDirectory(root *os.Root, name string) error {
	directory, err := root.Open(name)
	if err != nil {
		return fmt.Errorf("open backup directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync backup directory: %w", err)
	}
	return nil
}

func isDirectoryNotEmpty(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST)
}

type rootedArchiveReader struct {
	*os.File
	root *os.Root
}

func (reader *rootedArchiveReader) Close() error {
	fileErr := reader.File.Close()
	rootErr := reader.root.Close()
	return errors.Join(fileErr, rootErr)
}
