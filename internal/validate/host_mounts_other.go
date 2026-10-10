//go:build !linux

package validate

import "fmt"

func hostIntegrationAccess(path, kind string, readOnly bool, uid, gid int) error {
	return fmt.Errorf("host integration access checks require Linux: %s", path)
}
