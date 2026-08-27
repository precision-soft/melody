package logging

import (
    "errors"
    "fmt"
    "os"
    "os/signal"
    "sync"

    "github.com/precision-soft/melody/v3/exception"
)

/* ErrRotatedDescriptorNotClosed marks the failure Reopen reports after the rotation already happened: the fresh descriptor is installed and taking the writes, and only the descriptor it replaced refused to close — a deferred write surfacing at close, the shape a remote or failing filesystem takes. The journal did rotate, so a caller reading this must not announce one that could not. */
var ErrRotatedDescriptorNotClosed = errors.New("the descriptor replaced by the rotation could not be closed")

/* NewReopenableFileWriter opens the file for appending and answers a writer that can be told to open it again under the same path. The reopen is what rename-based rotation needs: a rotator that moves the file away leaves a plain descriptor writing into the renamed inode, so the journal keeps flowing into a file no reader watches anymore — reopening after the rename points the writer at the fresh file the path now names. Every write, the reopen and the close serialize on one lock, so a record is never torn across the swap and never lands on a closed descriptor. */
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

/* Reopen opens the path fresh and swaps the descriptor, closing the one it replaces. A path that cannot be opened keeps the current descriptor and reports the failure, because a journal writing into a renamed file is still a journal, while one whose descriptor was surrendered before the replacement existed is silence. The two failures it can report are different events and are not interchangeable: an open failure means the rotation did not happen, while a descriptor that refuses to close after the swap means it did — the writer is already on the fresh file and healthy. Only the second carries ErrRotatedDescriptorNotClosed, so a caller separates them with errors.Is instead of reading every non-nil result as a journal that failed to rotate. */
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
        /* the swap above is already committed and is not rolled back: the fresh descriptor is the one the writer holds, so this reports a rotation that happened and a surrendered descriptor that reported a deferred write on its way out */
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

/* ArmReopenOnSignal registers the given signals and reopens the file each time one arrives; SIGHUP is the conventional choice, the signal logrotate sends after a rename. A reopen that fails is reported on stderr — this writer usually IS the journal, so the failure cannot be filed through it — and the descriptor in use keeps writing. A rotation that completed and only failed to close the descriptor it replaced is announced apart from one that did not rotate at all, so a deferred write surfacing at close is never read as a journal stuck on the renamed file. The watcher belongs to the writer: Close unregisters the signals and joins the goroutine, so a torn-down application leaves no watcher armed to act on a later signal, and arming twice is refused because two watchers would race each other's reopen. */
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

/* Close disarms the signal watcher before it surrenders the descriptor, in that order: a signal landing between the two would otherwise find the writer already refusing, and one landing after the surrender would reopen a file nobody owns. It is safe to call more than once. */
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
    /* a watcher armed between the first look and this lock would outlive the writer; the re-check under the same lock that Arm takes makes that window observable */
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
