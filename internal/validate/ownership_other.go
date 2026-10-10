//go:build !linux && !darwin

package validate

import "fmt"

func hostDirectoryAccess(path string) error {
	return fmt.Errorf("host access checks unsupported on this platform: %s", path)
}
