//go:build linux

package cron

import "syscall"

/* hasAccessControlList answers whether the directory carries a POSIX access control list, whose entries the mode bits do not show */
func hasAccessControlList(path string) bool {
    size, getErr := syscall.Getxattr(path, "system.posix_acl_access", nil)

    return nil == getErr && 0 < size
}
