//go:build linux || darwin

package validate

import (
	"fmt"
	"os"
	"syscall"
)

func hostDirectoryAccess(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory")
	}
	// W_OK | X_OK checks the current process's real identity and host ACLs.
	return syscall.Access(path, 3)
}
