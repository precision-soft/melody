//go:build unix

package session

import (
    "os"

    "golang.org/x/sys/unix"
)

/* fileHandleWritable reads the descriptor's access mode with fcntl F_GETFL through golang.org/x/sys/unix, which reaches it through the C library on the platforms that keep the system call behind it, and through the raw connection rather than Fd, which would put the file into blocking mode. An error means the mode could not be read. */
func fileHandleWritable(fileInstance *os.File) (bool, error) {
    rawConnection, connectionErr := fileInstance.SyscallConn()
    if nil != connectionErr {
        return true, connectionErr
    }

    var flags int
    var fcntlErr error

    controlErr := rawConnection.Control(func(descriptor uintptr) {
        flags, fcntlErr = unix.FcntlInt(descriptor, unix.F_GETFL, 0)
    })
    if nil != controlErr {
        return true, controlErr
    }

    if nil != fcntlErr {
        return true, fcntlErr
    }

    return unix.O_RDONLY != flags&unix.O_ACCMODE, nil
}
