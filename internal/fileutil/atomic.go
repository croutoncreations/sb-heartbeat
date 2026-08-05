package fileutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type targetError struct {
	err error
}

func (e *targetError) Error() string { return e.err.Error() }
func (e *targetError) Unwrap() error { return e.err }

func IsTargetError(err error) bool {
	var targetErr *targetError
	return errors.As(err, &targetErr)
}

func invalidTarget(format string, args ...any) error {
	return &targetError{err: fmt.Errorf(format, args...)}
}

func WriteAtomic(path string, data []byte, mode os.FileMode, force bool) error {
	if err := CheckTarget(path, force); err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	temporary, err := os.CreateTemp(dir, ".sb-heartbeat-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return fmt.Errorf("set temporary file mode: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := commitTemporary(temporaryPath, path, force); err != nil {
		return err
	}
	return nil
}

func commitTemporary(temporaryPath, path string, force bool) error {
	if force {
		if err := os.Rename(temporaryPath, path); err != nil {
			return fmt.Errorf("replace output file: %w", err)
		}
		return nil
	}
	if err := os.Link(temporaryPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return invalidTarget("file already exists: %s: %w", path, os.ErrExist)
		}
		return fmt.Errorf("install output file without replacement: %w", err)
	}
	return nil
}

func CheckTarget(path string, force bool) error {
	if path == "" {
		return invalidTarget("output path is empty")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return invalidTarget("refusing to overwrite symlink %s", path)
		}
		if !info.Mode().IsRegular() {
			return invalidTarget("refusing to overwrite non-regular file %s", path)
		}
		if !force {
			return invalidTarget("file already exists: %s: %w", path, os.ErrExist)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output path: %w", err)
	}

	return nil
}
