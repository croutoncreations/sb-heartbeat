//go:build !darwin && !linux

package envfile

import (
	"errors"
	"os"
)

func openNoFollow(string) (*os.File, error) {
	return nil, errors.New("private environment files are supported only on macOS and Linux")
}
