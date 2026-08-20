//go:build !darwin && !linux && !windows

package secretstore

import (
	"fmt"
	"runtime"
)

func Open() (Store, error) {
	return nil, fmt.Errorf("no credential-store implementation for %s", runtime.GOOS)
}
