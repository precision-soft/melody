//go:build !unix

package cron

import "os"

/* directoryOwner answers no owner on a platform without unix ownership, where the logs directory is not judged */
func directoryOwner(information os.FileInfo) (string, string, bool) {
    return "", "", false
}
