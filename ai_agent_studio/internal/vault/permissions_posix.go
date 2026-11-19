//go:build !windows

package vault

import (
	"errors"
	"os"
)

func createPrivateDirectory(path string) error { return os.MkdirAll(path, 0o700) }

func createPrivateFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

func verifyPrivatePath(_ string, info os.FileInfo, directory bool) error {
	if info.Mode().Perm()&0o077 != 0 || (!directory && info.Mode().Perm()&0o400 == 0) {
		return errors.New("vault directory/key must be private to the service owner (0700/0600)")
	}
	return nil
}
