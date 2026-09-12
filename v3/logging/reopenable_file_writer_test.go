package logging

import (
    "errors"
    "os"
    "path/filepath"
    "strings"
    "syscall"
    "testing"
    "time"
)

func TestReopenableFileWriter_WritesToTheFileItOpened(t *testing.T) {
    logPath := filepath.Join(t.TempDir(), "application.log")

    writer, newErr := NewReopenableFileWriter(logPath)
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.Close() }()

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

    writer, newErr := NewReopenableFileWriter(logPath)
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.Close() }()

    _, _ = writer.Write([]byte("before rotation\n"))

    renameErr := os.Rename(logPath, rotatedPath)
    if nil != renameErr {
        t.Fatalf("unexpected rename error: %v", renameErr)
    }

    /* without the reopen this write lands in the renamed inode — that is the defect the writer exists for */
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

    writer, newErr := NewReopenableFileWriter(logPath)
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.Close() }()

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

    writer, newErr := NewReopenableFileWriter(logPath)
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }

    if closeErr := writer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }
    if closeErr := writer.Close(); nil != closeErr {
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

    writer, newErr := NewReopenableFileWriter(logPath)
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.Close() }()

    if armErr := writer.ArmReopenOnSignal(); nil == armErr {
        t.Fatalf("expected the empty signal set to be refused")
    }

    if armErr := writer.ArmReopenOnSignal(syscall.SIGHUP); nil != armErr {
        t.Fatalf("unexpected arm error: %v", armErr)
    }

    secondArmErr := writer.ArmReopenOnSignal(syscall.SIGHUP)
    if nil == secondArmErr {
        t.Fatalf("expected the second arm to be refused")
    }
    if false == strings.Contains(secondArmErr.Error(), "already armed") {
        t.Fatalf("expected the refusal to name the armed watcher, got %v", secondArmErr)
    }
}

/* the live half: a real SIGHUP delivered to this process reopens the journal. The probe polls by writing markers — every write flows through whatever descriptor is current, so the marker appears at the path exactly when the watcher's reopen has swapped it. The signal is only ever sent while the watcher is armed: an armed Notify holds the process's default SIGHUP action off, and Close disarms before this test returns. */
func TestReopenableFileWriter_ASighupReopensTheJournalWhileArmed(t *testing.T) {
    directory := t.TempDir()
    logPath := filepath.Join(directory, "application.log")
    rotatedPath := filepath.Join(directory, "application.log.1")

    writer, newErr := NewReopenableFileWriter(logPath)
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.Close() }()

    if armErr := writer.ArmReopenOnSignal(syscall.SIGHUP); nil != armErr {
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

/* the swap is committed before the replaced descriptor is closed and is never rolled back, so a refused close reports a rotation that HAPPENED. Sharing a headline with a failed open told the operator the journal was stuck on the renamed file while it was in fact healthy on the fresh one. */
func TestReopenableFileWriter_ARefusedCloseOfTheReplacedDescriptorIsStillARotation(t *testing.T) {
    directory := t.TempDir()
    logPath := filepath.Join(directory, "application.log")
    rotatedPath := filepath.Join(directory, "application.log.1")

    writer, newErr := NewReopenableFileWriter(logPath)
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.Close() }()

    _, _ = writer.Write([]byte("before rotation\n"))

    renameErr := os.Rename(logPath, rotatedPath)
    if nil != renameErr {
        t.Fatalf("unexpected rename error: %v", renameErr)
    }

    /* surrender the descriptor under the writer so the rotation's own close of it refuses — the shape a deferred write surfacing at close(2) takes on a remote or failing device */
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

    /* the writer is on the fresh file and healthy, which is exactly what the sentinel promises the caller */
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

/* the negative half: a rotation that did NOT happen must never carry the sentinel, or the caller announces a healthy journal while the writer is still on the renamed inode */
func TestReopenableFileWriter_AFailedOpenDoesNotCarryTheRotatedDescriptorSentinel(t *testing.T) {
    directory := t.TempDir()
    logPath := filepath.Join(directory, "application.log")
    rotatedPath := filepath.Join(directory, "application.log.1")

    writer, newErr := NewReopenableFileWriter(logPath)
    if nil != newErr {
        t.Fatalf("unexpected constructor error: %v", newErr)
    }
    defer func() { _ = writer.Close() }()

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

    closedWriter, closedWriterErr := NewReopenableFileWriter(filepath.Join(directory, "other.log"))
    if nil != closedWriterErr {
        t.Fatalf("unexpected constructor error: %v", closedWriterErr)
    }
    if closeErr := closedWriter.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if true == errors.Is(closedWriter.Reopen(), ErrRotatedDescriptorNotClosed) {
        t.Fatalf("expected a closed writer's refusal to stay a failed rotation")
    }
}
