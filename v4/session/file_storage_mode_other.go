//go:build !unix

package session

import (
    "os"
)

/* fileHandleWritable answers writable where the descriptor's access mode is not read: a read-only handle passes construction there, and its first Save reports it. */
func fileHandleWritable(fileInstance *os.File) (bool, error) {
    return true, nil
}
