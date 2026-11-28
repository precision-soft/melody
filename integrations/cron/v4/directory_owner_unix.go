//go:build unix

package cron

import (
    "os"
    "strconv"
    "syscall"
)

/* directoryOwner answers the uid and gid owning a directory, false where the platform does not report them */
func directoryOwner(information os.FileInfo) (string, string, bool) {
    status, isStatus := information.Sys().(*syscall.Stat_t)
    if false == isStatus {
        return "", "", false
    }

    return strconv.FormatUint(uint64(status.Uid), 10), strconv.FormatUint(uint64(status.Gid), 10), true
}
