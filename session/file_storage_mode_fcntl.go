//go:build linux || darwin || freebsd || netbsd || dragonfly

package session

import (
    "os"
    "syscall"
)

/* fileHandleWritable reads the descriptor's access mode with fcntl F_GETFL, through the raw connection rather than Fd, which would put the file into blocking mode. An error means the mode could not be read. */
func fileHandleWritable(fileInstance *os.File) (bool, error) {
    rawConnection, connectionErr := fileInstance.SyscallConn()
    if nil != connectionErr {
        return true, connectionErr
    }

    var flags uintptr
    var errno syscall.Errno

    controlErr := rawConnection.Control(func(descriptor uintptr) {
        flags, _, errno = syscall.Syscall(syscall.SYS_FCNTL, descriptor, syscall.F_GETFL, 0)
    })
    if nil != controlErr {
        return true, controlErr
    }

    if 0 != errno {
        return true, errno
    }

    return syscall.O_RDONLY != int(flags)&syscall.O_ACCMODE, nil
}
