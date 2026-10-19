package logging

import (
    "errors"
    "fmt"
    "os"
    "os/signal"
    "sync"

    "github.com/precision-soft/melody/v3/exception"
)

/* ErrRotatedDescriptorNotClosed marks the failure Reopen reports after the rotation happened: the fresh descriptor takes the writes, and only the replaced one refused to close. A caller reading it must not report a failed rotation. */
var ErrRotatedDescriptorNotClosed = errors.New("the descriptor replaced by the rotation could not be closed")

/* NewReopenableFileWriter opens the file for appending and answers a writer that can reopen it under the same path, which rename-based rotation needs. Every write, the reopen and the close serialize on one lock, so a record is never torn across the swap nor written to a closed descriptor. */
func NewReopenableFileWriter(path string) (*ReopenableFileWriter, error) {
    file, openErr := openReopenableFile(path)
    if nil != openErr {
        return nil, openErr
    }

    return &ReopenableFileWriter{
        path: path,
        file: file,
    }, nil
}

type ReopenableFileWriter struct {
    mutex  sync.Mutex
    path   string
    file   *os.File
    closed bool

    watcherArmed  bool
    signalChannel chan os.Signal
    stopChannel   chan struct{}
    doneChannel   chan struct{}
    stopOnce      sync.Once
}

func (instance *ReopenableFileWriter) Write(payload []byte) (int, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return 0, exception.NewError(
            "reopenable file writer is closed",
            map[string]any{
                "path": instance.path,
            },
            nil,
        )
    }

    return instance.file.Write(payload)
}

/* Reopen opens the path fresh, swaps the descriptor and closes the one it replaces. A path that cannot be opened keeps the current descriptor and reports the failure, meaning no rotation; a replaced descriptor that refuses to close after the swap reports ErrRotatedDescriptorNotClosed, meaning the rotation happened. */
func (instance *ReopenableFileWriter) Reopen() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return exception.NewError(
            "reopenable file writer is closed",
            map[string]any{
                "path": instance.path,
            },
            nil,
        )
    }

    freshFile, openErr := openReopenableFile(instance.path)
    if nil != openErr {
        return openErr
    }

    previousFile := instance.file
    instance.file = freshFile

    closeErr := previousFile.Close()
    if nil != closeErr {
        /* the swap is committed and not rolled back: the fresh descriptor is the one the writer holds */
        return exception.NewError(
            "failed to close the descriptor replaced by the log rotation",
            map[string]any{
                "path": instance.path,
            },
            errors.Join(closeErr, ErrRotatedDescriptorNotClosed),
        )
    }

    return nil
}

/* ArmReopenOnSignal reopens the file each time one of the signals arrives; SIGHUP is the one logrotate sends. A failed reopen is reported on stderr, since this writer is usually the journal, and the descriptor in use keeps writing; ErrRotatedDescriptorNotClosed is announced apart. Close unregisters the signals and joins the watcher, and arming twice is refused, since two watchers would race each other's reopen. */
func (instance *ReopenableFileWriter) ArmReopenOnSignal(signals ...os.Signal) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return exception.NewError(
            "reopenable file writer is closed",
            map[string]any{
                "path": instance.path,
            },
            nil,
        )
    }

    if true == instance.watcherArmed {
        return exception.NewError(
            "reopen signal watcher is already armed",
            map[string]any{
                "path": instance.path,
            },
            nil,
        )
    }

    if 0 == len(signals) {
        return exception.NewError(
            "reopen signal watcher needs at least one signal",
            map[string]any{
                "path": instance.path,
            },
            nil,
        )
    }

    instance.watcherArmed = true
    instance.signalChannel = make(chan os.Signal, 1)
    instance.stopChannel = make(chan struct{})
    instance.doneChannel = make(chan struct{})

    signal.Notify(instance.signalChannel, signals...)

    go instance.watchReopenSignals()

    return nil
}

func (instance *ReopenableFileWriter) watchReopenSignals() {
    defer close(instance.doneChannel)

    for {
        select {
        case <-instance.stopChannel:
            return

        case _, open := <-instance.signalChannel:
            if false == open {
                return
            }

            reopenErr := instance.Reopen()
            if nil != reopenErr {
                if true == errors.Is(reopenErr, ErrRotatedDescriptorNotClosed) {
                    fmt.Fprintf(os.Stderr, "melody: the log journal rotated, and the descriptor it replaced could not be closed: %v\n", reopenErr)
                } else {
                    fmt.Fprintf(os.Stderr, "melody: the log journal could not be reopened after a rotation signal: %v\n", reopenErr)
                }
            }
        }
    }
}

/* Close disarms the signal watcher before it surrenders the descriptor, in that order, so no signal reopens a file nobody owns. It is safe to call more than once. */
func (instance *ReopenableFileWriter) Close() error {
    instance.mutex.Lock()
    alreadyClosed := instance.closed
    watcherArmed := instance.watcherArmed
    instance.mutex.Unlock()

    if true == alreadyClosed {
        return nil
    }

    if true == watcherArmed {
        instance.disarmReopenWatcher()
    }

    instance.mutex.Lock()

    if true == instance.closed {
        instance.mutex.Unlock()
        return nil
    }

    instance.closed = true
    /* a watcher armed between the first look and this lock would outlive the writer, so the check is repeated under the lock Arm takes */
    watcherArmed = instance.watcherArmed
    file := instance.file

    instance.mutex.Unlock()

    if true == watcherArmed {
        instance.disarmReopenWatcher()
    }

    return file.Close()
}

func (instance *ReopenableFileWriter) disarmReopenWatcher() {
    instance.stopOnce.Do(
        func() {
            signal.Stop(instance.signalChannel)
            close(instance.stopChannel)
        },
    )
    <-instance.doneChannel
}

func openReopenableFile(path string) (*os.File, error) {
    file, openErr := os.OpenFile(
        path,
        os.O_CREATE|os.O_APPEND|os.O_WRONLY,
        0o644,
    )
    if nil != openErr {
        return nil, exception.NewError(
            "failed to open log file",
            map[string]any{
                "path": path,
            },
            openErr,
        )
    }

    return file, nil
}
