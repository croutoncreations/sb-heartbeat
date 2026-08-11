package envfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strings"
)

const MaxFileBytes = 64 * 1024

var namePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,126}$`)

// Load reads a dedicated environment file without evaluating it as shell code.
func Load(path string) (map[string]string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect environment file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("refusing to read environment file through a symlink")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("environment file path is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("environment file permissions must not allow group or other access")
	}
	if info.Size() > MaxFileBytes {
		return nil, fmt.Errorf("environment file is too large: maximum is %d bytes", MaxFileBytes)
	}

	file, err := openNoFollow(path)
	if err != nil {
		return nil, fmt.Errorf("open environment file: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect opened environment file: %w", err)
	}
	if !os.SameFile(info, openedInfo) {
		return nil, errors.New("environment file changed while opening")
	}
	if !openedInfo.Mode().IsRegular() {
		return nil, errors.New("opened environment file is not a regular file")
	}
	if runtime.GOOS != "windows" && openedInfo.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("opened environment file permissions must not allow group or other access")
	}
	if openedInfo.Size() > MaxFileBytes {
		return nil, fmt.Errorf("opened environment file is too large: maximum is %d bytes", MaxFileBytes)
	}
	contents, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read environment file: %w", err)
	}
	if len(contents) > MaxFileBytes {
		return nil, fmt.Errorf("environment file is too large: maximum is %d bytes", MaxFileBytes)
	}
	if strings.IndexByte(string(contents), 0) >= 0 {
		return nil, errors.New("environment file contains a NUL byte")
	}

	values := make(map[string]string)
	lines := strings.Split(string(contents), "\n")
	for index, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if strings.ContainsRune(line, '\r') {
			return nil, fmt.Errorf("environment file line %d contains an embedded carriage return", index+1)
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found || !namePattern.MatchString(name) {
			return nil, fmt.Errorf("environment file line %d is not a valid NAME=value entry", index+1)
		}
		if _, exists := values[name]; exists {
			return nil, fmt.Errorf("environment file line %d duplicates %s", index+1, name)
		}
		values[name] = value
	}
	return values, nil
}
