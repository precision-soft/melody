package logging

import (
    "context"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "syscall"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

func TestReopenableFileWriter_WritesToTheFileItOpened(t *testing.T) {
    logPath := filepath.Join(t.TempDir(), "application.log")

    writer, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{})
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.CloseWithContext(context.Background()) }()

    _, writeErr := writer.Write([]byte("first record\n"))
    if nil != writeErr {
        t.Fatalf("unexpected write error: %v", writeErr)
    }

    content, readErr := os.ReadFile(logPath)
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }
    if "first record\n" != string(content) {
        t.Fatalf("expected the record in the file, got %q", string(content))
    }
}

func TestReopenableFileWriter_ReopenFollowsARenameToTheFreshFile(t *testing.T) {
    directory := t.TempDir()
    logPath := filepath.Join(directory, "application.log")
    rotatedPath := filepath.Join(directory, "application.log.1")

    writer, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{})
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.CloseWithContext(context.Background()) }()

    _, _ = writer.Write([]byte("before rotation\n"))

    renameErr := os.Rename(logPath, rotatedPath)
    if nil != renameErr {
        t.Fatalf("unexpected rename error: %v", renameErr)
    }

    reopenErr := writer.Reopen()
    if nil != reopenErr {
        t.Fatalf("unexpected reopen error: %v", reopenErr)
    }

    _, writeErr := writer.Write([]byte("after rotation\n"))
    if nil != writeErr {
        t.Fatalf("unexpected write error: %v", writeErr)
    }

    rotatedContent, _ := os.ReadFile(rotatedPath)
    if "before rotation\n" != string(rotatedContent) {
        t.Fatalf("expected the rotated file to keep only the first record, got %q", string(rotatedContent))
    }

    freshContent, readErr := os.ReadFile(logPath)
    if nil != readErr {
        t.Fatalf("expected a fresh file at the path after the reopen, got %v", readErr)
    }
    if "after rotation\n" != string(freshContent) {
        t.Fatalf("expected the second record in the fresh file, got %q", string(freshContent))
    }
}

func TestReopenableFileWriter_AFailedReopenKeepsTheCurrentDescriptorWriting(t *testing.T) {
    directory := t.TempDir()
    logPath := filepath.Join(directory, "application.log")
    rotatedPath := filepath.Join(directory, "application.log.1")

    writer, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{})
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.CloseWithContext(context.Background()) }()

    renameErr := os.Rename(logPath, rotatedPath)
    if nil != renameErr {
        t.Fatalf("unexpected rename error: %v", renameErr)
    }

    /* a directory at the path makes the append open fail whoever runs the test — a permission probe does not, because the container test user is root */
    mkdirErr := os.Mkdir(logPath, 0o755)
    if nil != mkdirErr {
        t.Fatalf("unexpected mkdir error: %v", mkdirErr)
    }

    reopenErr := writer.Reopen()
    if nil == reopenErr {
        t.Fatalf("expected the reopen to fail over a directory at the path")
    }

    _, writeErr := writer.Write([]byte("still flowing\n"))
    if nil != writeErr {
        t.Fatalf("expected the current descriptor to keep writing, got %v", writeErr)
    }

    rotatedContent, _ := os.ReadFile(rotatedPath)
    if false == strings.Contains(string(rotatedContent), "still flowing") {
        t.Fatalf("expected the record in the renamed file the descriptor still points at, got %q", string(rotatedContent))
    }
}

func TestReopenableFileWriter_CloseRefusesLaterWritesAndStaysIdempotent(t *testing.T) {
    logPath := filepath.Join(t.TempDir(), "application.log")

    writer, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{})
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }

    if closeErr := writer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }
    if closeErr := writer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("expected the second close to answer nil, got %v", closeErr)
    }

    _, writeErr := writer.Write([]byte("late record\n"))
    if nil == writeErr {
        t.Fatalf("expected the closed writer to refuse the write")
    }
    if false == strings.Contains(writeErr.Error(), "reopenable file writer is closed") {
        t.Fatalf("expected the refusal to name the closed writer, got %v", writeErr)
    }

    reopenErr := writer.Reopen()
    if nil == reopenErr {
        t.Fatalf("expected the closed writer to refuse the reopen")
    }
}

func TestReopenableFileWriter_ArmRefusesADoubleArmAndAnEmptySignalSet(t *testing.T) {
    logPath := filepath.Join(t.TempDir(), "application.log")

    writer, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{})
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.CloseWithContext(context.Background()) }()

    if armErr := writer.ArmReopenOnSignal(nil); nil == armErr {
        t.Fatalf("expected the empty signal set to be refused")
    }

    if armErr := writer.ArmReopenOnSignal([]os.Signal{syscall.SIGHUP}); nil != armErr {
        t.Fatalf("unexpected arm error: %v", armErr)
    }

    secondArmErr := writer.ArmReopenOnSignal([]os.Signal{syscall.SIGHUP})
    if nil == secondArmErr {
        t.Fatalf("expected the second arm to be refused")
    }
    if false == strings.Contains(secondArmErr.Error(), "already armed") {
        t.Fatalf("expected the refusal to name the armed watcher, got %v", secondArmErr)
    }
}

func TestReopenableFileWriter_ASighupReopensTheJournalWhileArmed(t *testing.T) {
    directory := t.TempDir()
    logPath := filepath.Join(directory, "application.log")
    rotatedPath := filepath.Join(directory, "application.log.1")

    writer, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{})
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.CloseWithContext(context.Background()) }()

    if armErr := writer.ArmReopenOnSignal([]os.Signal{syscall.SIGHUP}); nil != armErr {
        t.Fatalf("unexpected arm error: %v", armErr)
    }

    renameErr := os.Rename(logPath, rotatedPath)
    if nil != renameErr {
        t.Fatalf("unexpected rename error: %v", renameErr)
    }

    killErr := syscall.Kill(os.Getpid(), syscall.SIGHUP)
    if nil != killErr {
        t.Fatalf("unexpected kill error: %v", killErr)
    }

    deadline := time.Now().Add(5 * time.Second)
    for {
        _, writeErr := writer.Write([]byte("marker\n"))
        if nil != writeErr {
            t.Fatalf("unexpected write error: %v", writeErr)
        }

        content, readErr := os.ReadFile(logPath)
        if nil == readErr && true == strings.Contains(string(content), "marker") {
            return
        }

        if true == time.Now().After(deadline) {
            t.Fatalf("the journal was not reopened after the signal; the path answered %v", readErr)
        }

        time.Sleep(10 * time.Millisecond)
    }
}

func TestReopenableFileWriter_ARefusedCloseOfTheReplacedDescriptorIsStillARotation(t *testing.T) {
    directory := t.TempDir()
    logPath := filepath.Join(directory, "application.log")
    rotatedPath := filepath.Join(directory, "application.log.1")

    writer, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{})
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.CloseWithContext(context.Background()) }()

    _, _ = writer.Write([]byte("before rotation\n"))

    renameErr := os.Rename(logPath, rotatedPath)
    if nil != renameErr {
        t.Fatalf("unexpected rename error: %v", renameErr)
    }

    /* surrender the descriptor under the writer so the rotation's own close of it refuses */
    writer.mutex.Lock()
    _ = writer.file.Close()
    writer.mutex.Unlock()

    reopenErr := writer.Reopen()
    if nil == reopenErr {
        t.Fatalf("expected the refused close of the replaced descriptor to be reported")
    }

    if false == errors.Is(reopenErr, ErrRotatedDescriptorNotClosed) {
        t.Fatalf("expected the refused close to be told apart from a failed rotation, got %v", reopenErr)
    }

    _, writeErr := writer.Write([]byte("after rotation\n"))
    if nil != writeErr {
        t.Fatalf("expected the rotated writer to keep writing, got %v", writeErr)
    }

    freshContent, readErr := os.ReadFile(logPath)
    if nil != readErr {
        t.Fatalf("expected a fresh file at the path after the rotation, got %v", readErr)
    }
    if "after rotation\n" != string(freshContent) {
        t.Fatalf("expected the record in the fresh file, got %q", string(freshContent))
    }

    rotatedContent, _ := os.ReadFile(rotatedPath)
    if "before rotation\n" != string(rotatedContent) {
        t.Fatalf("expected the renamed file to keep only the first record, got %q", string(rotatedContent))
    }
}

func TestReopenableFileWriter_AFailedOpenDoesNotCarryTheRotatedDescriptorSentinel(t *testing.T) {
    directory := t.TempDir()
    logPath := filepath.Join(directory, "application.log")
    rotatedPath := filepath.Join(directory, "application.log.1")

    writer, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{})
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.CloseWithContext(context.Background()) }()

    renameErr := os.Rename(logPath, rotatedPath)
    if nil != renameErr {
        t.Fatalf("unexpected rename error: %v", renameErr)
    }

    /* a directory at the path makes the append open fail whoever runs the test — a permission probe does not, because the container test user is root */
    mkdirErr := os.Mkdir(logPath, 0o755)
    if nil != mkdirErr {
        t.Fatalf("unexpected mkdir error: %v", mkdirErr)
    }

    reopenErr := writer.Reopen()
    if nil == reopenErr {
        t.Fatalf("expected the reopen to fail over a directory at the path")
    }

    if true == errors.Is(reopenErr, ErrRotatedDescriptorNotClosed) {
        t.Fatalf("expected a failed open to stay a failed rotation, got %v", reopenErr)
    }

    closedWriter, closedWriterErr := NewReopenableFileWriter(filepath.Join(directory, "other.log"), ReopenableFileWriterConfig{})
    if nil != closedWriterErr {
        t.Fatalf("unexpected constructor error: %v", closedWriterErr)
    }
    if closeErr := closedWriter.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if true == errors.Is(closedWriter.Reopen(), ErrRotatedDescriptorNotClosed) {
        t.Fatalf("expected a closed writer's refusal to stay a failed rotation")
    }
}

func TestReopenableFileWriter_WritesStayWholeWhileReopenSwapsTheDescriptor(t *testing.T) {
    const recordCount = 2000
    const reopenCount = 200

    logPath := filepath.Join(t.TempDir(), "application.log")

    writer, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{})
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.CloseWithContext(context.Background()) }()

    release := make(chan struct{})

    var ready sync.WaitGroup
    var done sync.WaitGroup

    ready.Add(2)
    done.Add(2)

    go func() {
        defer done.Done()

        ready.Done()
        <-release

        for index := 0; index < recordCount; index++ {
            if _, writeErr := writer.Write([]byte(fmt.Sprintf("record-%04d\n", index))); nil != writeErr {
                t.Errorf("write %d failed while the descriptor was reopened: %v", index, writeErr)
                return
            }
        }
    }()

    go func() {
        defer done.Done()

        ready.Done()
        <-release

        for index := 0; index < reopenCount; index++ {
            if reopenErr := writer.Reopen(); nil != reopenErr {
                t.Errorf("reopen %d failed: %v", index, reopenErr)
                return
            }
        }
    }()

    ready.Wait()
    close(release)
    done.Wait()

    content, readErr := os.ReadFile(logPath)
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
    if recordCount != len(lines) {
        t.Fatalf("expected %d records in the file, got %d", recordCount, len(lines))
    }

    for index, line := range lines {
        if fmt.Sprintf("record-%04d", index) != line {
            t.Fatalf("expected record %d whole and in order, got %q", index, line)
        }
    }
}

func TestReopenableFileWriter_TheContainerClosesItThroughItsContextDoor(t *testing.T) {
    writer, writerErr := NewReopenableFileWriter(filepath.Join(t.TempDir(), "app.log"), ReopenableFileWriterConfig{})
    if nil != writerErr {
        t.Fatalf("unexpected writer error: %v", writerErr)
    }

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[*ReopenableFileWriter](
        serviceContainer,
        "app.log.writer",
        func(resolver containercontract.Resolver) (*ReopenableFileWriter, error) {
            return writer, nil
        },
        containercontract.RegisterOptions{},
    )

    container.MustFromResolver[*ReopenableFileWriter](context.Background(), serviceContainer, "app.log.writer")

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if _, writeErr := writer.Write([]byte("late")); nil == writeErr {
        t.Fatalf("expected the writer closed by the teardown")
    }
}

func TestReopenableFileWriter_CloseWithContextEndsAtItsDeadlineWhenTheCloseIsStuck(t *testing.T) {
    writer, writerErr := NewReopenableFileWriter(filepath.Join(t.TempDir(), "app.log"), ReopenableFileWriterConfig{})
    if nil != writerErr {
        t.Fatalf("unexpected writer error: %v", writerErr)
    }

    /* the mutex a Reopen on a stuck filesystem would hold */
    writer.mutex.Lock()

    closeContext, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
    defer cancel()

    closeErr := writer.CloseWithContext(closeContext)

    writer.mutex.Unlock()

    if false == errors.Is(closeErr, context.DeadlineExceeded) {
        t.Fatalf("expected the deadline, got %v", closeErr)
    }
}

func TestReopenableFileWriter_CloseWithContextRefusesANilContext(t *testing.T) {
    writer, writerErr := NewReopenableFileWriter(filepath.Join(t.TempDir(), "app.log"), ReopenableFileWriterConfig{})
    if nil != writerErr {
        t.Fatalf("unexpected writer error: %v", writerErr)
    }
    defer func() { _ = writer.CloseWithContext(context.Background()) }()

    //nolint:staticcheck // the nil context is the input under test
    if closeErr := writer.CloseWithContext(nil); nil == closeErr || "the close context is nil" != closeErr.Error() || "ReopenableFileWriter.CloseWithContext" != exception.FromError(closeErr).Context()["door"] {
        t.Fatalf("expected the nil context refused naming the door, got %v", closeErr)
    }
}

func TestReopenableFileWriter_CloseWithContextDisarmsThenSurrendersOnce(t *testing.T) {
    writer, writerErr := NewReopenableFileWriter(filepath.Join(t.TempDir(), "app.log"), ReopenableFileWriterConfig{})
    if nil != writerErr {
        t.Fatalf("unexpected writer error: %v", writerErr)
    }

    if armErr := writer.ArmReopenOnSignal([]os.Signal{syscall.SIGUSR2}); nil != armErr {
        t.Fatalf("unexpected arm error: %v", armErr)
    }

    if closeErr := writer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    select {
    case <-writer.doneChannel:
    default:
        t.Fatalf("expected the watcher disarmed before the close answered")
    }

    if _, statErr := writer.file.Stat(); nil == statErr {
        t.Fatalf("expected the descriptor surrendered")
    }

    if closeErr := writer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("expected a second close to answer nil, got %v", closeErr)
    }

    var plainCloser any = writer
    if _, isCloser := plainCloser.(io.Closer); true == isCloser {
        t.Fatalf("expected the writer to have one close door, CloseWithContext")
    }
}

func TestNewReopenableFileWriter_TheDefaultsAre0640And0750(t *testing.T) {
    withZeroUmask(t)

    logsDirectory := filepath.Join(t.TempDir(), "var", "log")
    logPath := filepath.Join(logsDirectory, "application.log")

    writer, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{})
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.CloseWithContext(context.Background()) }()

    assertPermission(t, logPath, DefaultLogFileMode)
    assertPermission(t, logsDirectory, DefaultLogDirectoryMode)
    assertPermission(t, filepath.Dir(logsDirectory), DefaultLogDirectoryMode)

    if os.FileMode(0o640) != DefaultLogFileMode || os.FileMode(0o750) != DefaultLogDirectoryMode {
        t.Fatalf("expected the defaults 0640 and 0750, got %o and %o", DefaultLogFileMode, DefaultLogDirectoryMode)
    }
}

func TestNewReopenableFileWriter_CreatesTheFileAndItsDirectoriesAtTheConfiguredModes(t *testing.T) {
    withZeroUmask(t)

    logsDirectory := filepath.Join(t.TempDir(), "var", "log")
    logPath := filepath.Join(logsDirectory, "application.log")

    writer, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{FileMode: 0o660, DirectoryMode: 0o770})
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.CloseWithContext(context.Background()) }()

    assertPermission(t, logPath, 0o660)
    assertPermission(t, logsDirectory, 0o770)
    assertPermission(t, filepath.Dir(logsDirectory), 0o770)
}

func TestNewReopenableFileWriter_AModeUnderTheFloorIsRefusedNamingTheField(t *testing.T) {
    for _, testCase := range []struct {
        config  ReopenableFileWriterConfig
        field   string
        value   string
        message string
    }{
        {config: ReopenableFileWriterConfig{FileMode: 0o400}, field: "ReopenableFileWriterConfig.FileMode", value: "0400", message: internal.JournalFileModeRefusal},
        {config: ReopenableFileWriterConfig{FileMode: 0o240}, field: "ReopenableFileWriterConfig.FileMode", value: "0240", message: internal.JournalFileModeRefusal},
        {config: ReopenableFileWriterConfig{FileMode: os.ModeSetuid | 0o640}, field: "ReopenableFileWriterConfig.FileMode", value: fmt.Sprintf("%#o", uint32(os.ModeSetuid|0o640)), message: internal.JournalFileModeRefusal},
        {config: ReopenableFileWriterConfig{DirectoryMode: 0o600}, field: "ReopenableFileWriterConfig.DirectoryMode", value: "0600", message: internal.JournalDirectoryModeRefusal},
        {config: ReopenableFileWriterConfig{DirectoryMode: 0o650}, field: "ReopenableFileWriterConfig.DirectoryMode", value: "0650", message: internal.JournalDirectoryModeRefusal},
        {config: ReopenableFileWriterConfig{DirectoryMode: os.ModeDir | 0o750}, field: "ReopenableFileWriterConfig.DirectoryMode", value: fmt.Sprintf("%#o", uint32(os.ModeDir|0o750)), message: internal.JournalDirectoryModeRefusal},
    } {
        logPath := filepath.Join(t.TempDir(), "log", "application.log")

        writer, newErr := NewReopenableFileWriter(logPath, testCase.config)
        if nil != writer {
            _ = writer.CloseWithContext(context.Background())
            t.Fatalf("%s: expected no writer", testCase.field)
        }

        refusal, isRefusal := newErr.(*exception.Error)
        if false == isRefusal || testCase.message != refusal.Message() {
            t.Fatalf("%s: expected the refusal %q, got %v", testCase.field, testCase.message, newErr)
        }

        if testCase.field != refusal.Context()["field"] || testCase.value != refusal.Context()["value"] {
            t.Fatalf("%s: expected the refusal to name the field and the value %s, got %v", testCase.field, testCase.value, refusal.Context())
        }

        if _, statErr := os.Stat(filepath.Dir(logPath)); false == os.IsNotExist(statErr) {
            t.Fatalf("%s: expected nothing created before the refusal, stat answered %v", testCase.field, statErr)
        }
    }
}

func TestNewReopenableFileWriter_ADirectoryItCannotCreateIsRefusedNamingIt(t *testing.T) {
    inTheWay := filepath.Join(t.TempDir(), "file")
    if writeErr := os.WriteFile(inTheWay, []byte("x"), 0o600); nil != writeErr {
        t.Fatalf("could not put a file in the way: %v", writeErr)
    }

    logPath := filepath.Join(inTheWay, "application.log")

    _, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{})

    refusal, isRefusal := newErr.(*exception.Error)
    if false == isRefusal || "failed to create the log directory" != refusal.Message() {
        t.Fatalf("expected the directory refused, got %v", newErr)
    }

    if inTheWay != refusal.Context()["directory"] || logPath != refusal.Context()["path"] {
        t.Fatalf("expected the refusal to name the path and the directory, got %v", refusal.Context())
    }
}

func TestReopen_RecreatesARemovedDirectoryAtTheConfiguredMode(t *testing.T) {
    withZeroUmask(t)

    logsDirectory := filepath.Join(t.TempDir(), "var", "log")
    logPath := filepath.Join(logsDirectory, "application.log")

    writer, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{FileMode: 0o620, DirectoryMode: 0o710})
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.CloseWithContext(context.Background()) }()

    if removeErr := os.RemoveAll(filepath.Dir(logsDirectory)); nil != removeErr {
        t.Fatalf("could not remove the directory: %v", removeErr)
    }

    if reopenErr := writer.Reopen(); nil != reopenErr {
        t.Fatalf("expected the reopen to recreate the directory, got %v", reopenErr)
    }

    assertPermission(t, logPath, 0o620)
    assertPermission(t, logsDirectory, 0o710)
    assertPermission(t, filepath.Dir(logsDirectory), 0o710)
}

func TestReopen_DoesNotChmodAnExistingFile(t *testing.T) {
    withZeroUmask(t)

    logsDirectory := filepath.Join(t.TempDir(), "log")
    if mkdirErr := os.Mkdir(logsDirectory, 0o755); nil != mkdirErr {
        t.Fatalf("could not create the directory: %v", mkdirErr)
    }

    logPath := filepath.Join(logsDirectory, "application.log")
    if writeErr := os.WriteFile(logPath, nil, 0o644); nil != writeErr {
        t.Fatalf("could not create the file: %v", writeErr)
    }

    writer, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{})
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.CloseWithContext(context.Background()) }()

    if reopenErr := writer.Reopen(); nil != reopenErr {
        t.Fatalf("unexpected reopen error: %v", reopenErr)
    }

    assertPermission(t, logPath, 0o644)
    assertPermission(t, logsDirectory, 0o755)
}

func TestReopen_RecreatesARotatedFileAtTheConfiguredMode(t *testing.T) {
    withZeroUmask(t)

    logPath := filepath.Join(t.TempDir(), "application.log")

    writer, newErr := NewReopenableFileWriter(logPath, ReopenableFileWriterConfig{FileMode: 0o600})
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.CloseWithContext(context.Background()) }()

    if renameErr := os.Rename(logPath, logPath+".1"); nil != renameErr {
        t.Fatalf("could not rotate: %v", renameErr)
    }

    if reopenErr := writer.Reopen(); nil != reopenErr {
        t.Fatalf("unexpected reopen error: %v", reopenErr)
    }

    assertPermission(t, logPath, 0o600)
}
