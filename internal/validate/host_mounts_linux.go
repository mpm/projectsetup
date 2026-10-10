//go:build linux

package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Use primary numeric IDs only: host supplementary groups are not guaranteed to
// exist in the container. These conservative checks do not infer container ACLs.
func hostIntegrationAccess(path, kind string, readOnly bool, uid, gid int) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	required := uint32(4)
	if !readOnly {
		required |= 2
	}
	if kind == "socket" {
		required = 2
	}
	if kind == "directory" {
		required |= 1
	}
	if err := hostIntegrationModeAccess(resolved, required, uid, gid); err != nil {
		return err
	}
	for parent := filepath.Dir(resolved); ; parent = filepath.Dir(parent) {
		if err := hostIntegrationModeAccess(parent, 1, uid, gid); err != nil {
			return err
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	return nil
}

func hostIntegrationModeAccess(path string, required uint32, uid, gid int) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot inspect numeric ownership of %q", path)
	}
	mode := uint32(info.Mode().Perm())
	allowed := mode & 7
	if uint32(uid) == stat.Uid {
		allowed = (mode >> 6) & 7
	} else if uint32(gid) == stat.Gid {
		allowed = (mode >> 3) & 7
	}
	// Linux UID 0 bypasses filesystem DAC; these probes never open a socket or
	// modify a source. Integration identity checks still report image mismatches.
	if uid == 0 {
		return nil
	}
	if allowed&required != required {
		return fmt.Errorf("%q needs permission bits %03o for IDs %d:%d (mode %03o, owner %d:%d)", path, required, uid, gid, mode, stat.Uid, stat.Gid)
	}
	return nil
}
