//go:build !linux

package cron

/* hasAccessControlList answers false where the access control list is not read, and the mode bits are judged alone */
func hasAccessControlList(path string) bool {
    return false
}
