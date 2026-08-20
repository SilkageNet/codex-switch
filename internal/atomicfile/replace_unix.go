//go:build !windows

package atomicfile

import "os"

func replace(source, destination string) error {
	return os.Rename(source, destination)
}

func syncParent(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
