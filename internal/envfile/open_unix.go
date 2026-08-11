//go:build darwin || linux

package envfile

import (
	"os"
	"syscall"
)

func openNoFollow(path string) (*os.File, error) {
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		syscall.Close(descriptor)
		return nil, os.ErrInvalid
	}
	return file, nil
}
