//go:build unix

package session

import (
    "os"
    "os/exec"
    "os/signal"
    "path/filepath"
    "strings"
    "syscall"
    "testing"
)

const fileStorageWriteWindowProbeMarker = "MELODY_SESSION_WRITE_WINDOW_PROBE"

/* the in-place writer must never leave the file empty: with a truncation to zero ahead of the write, a process killed between the two (an OOM kill, a docker kill, a deploy with no grace period) would leave a zero-length file that the next boot reads as "no sessions at all", logging every user out with no error anywhere. The kill is stood in for by a file size limit of zero, which is the only injection that reproduces it deterministically: a truncation to zero stays inside the limit and succeeds, while a write fails at its first byte. The limit is process-wide, so this runs in a child of its own. */
func TestFileStorage_InPlaceWrite_ARefusedWriteLeavesThePersistedSessionsIntact(t *testing.T) {
    if "1" == os.Getenv(fileStorageWriteWindowProbeMarker) {
        runFileStorageWriteWindowChild()

        return
    }

    command := exec.Command(
        os.Args[0],
        "-test.run=^TestFileStorage_InPlaceWrite_ARefusedWriteLeavesThePersistedSessionsIntact$",
    )
    command.Env = append(os.Environ(), fileStorageWriteWindowProbeMarker+"=1")

    output, runErr := command.CombinedOutput()
    if nil != runErr {
        t.Fatalf("expected the child to finish normally, got %v with output %q", runErr, string(output))
    }

    if true == strings.Contains(string(output), "emptied") {
        t.Fatalf("the refused write left the file empty, destroying every persisted session: %q", string(output))
    }

    /* the child answers a distinct token per exit, so a run that never applied the limit cannot be read as a pass: the parent refuses each of the other tokens by name before it asks for "intact". */
    if true == strings.Contains(string(output), "probe-unavailable") {
        t.Skipf("the environment refused the file size limit this probe injects with: %q", string(output))
    }

    if true == strings.Contains(string(output), "probe-did-not-inject") {
        t.Fatalf("the limited write succeeded, so nothing was injected and the guard was never exercised: %q", string(output))
    }

    if false == strings.Contains(string(output), "intact") {
        t.Fatalf("expected the child to report the file intact, got %q", string(output))
    }
}

func runFileStorageWriteWindowChild() {
    signal.Ignore(syscall.SIGXFSZ)

    /* a directory of its own, not a fixed name under os.TempDir: the three majors share one temp directory and their suites run concurrently, so a fixed name would have the children of two majors seed and remove the same file and one of them report a failed seed. */
    directory, directoryErr := os.MkdirTemp("", "melody_session_write_window")
    if nil != directoryErr {
        _, _ = os.Stdout.WriteString("temp-directory-failed\n")

        return
    }
    defer os.RemoveAll(directory)

    path := filepath.Join(directory, "sessions.json")

    seed, seedErr := NewFileStorageFromPath(path)
    if nil != seedErr {
        _, _ = os.Stdout.WriteString("seed-failed\n")

        return
    }

    if nil != seed.Save("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", map[string]any{"userId": "u-1"}, 0) {
        _, _ = os.Stdout.WriteString("seed-save-failed\n")

        return
    }

    _ = seed.Close()

    fileInstance, openErr := os.OpenFile(path, os.O_RDWR, 0o644)
    if nil != openErr {
        _, _ = os.Stdout.WriteString("open-failed\n")

        return
    }
    defer fileInstance.Close()

    storage, storageErr := NewFileStorageFromFile(fileInstance)
    if nil != storageErr {
        _, _ = os.Stdout.WriteString("storage-failed\n")

        return
    }

    var previous syscall.Rlimit
    if nil != syscall.Getrlimit(syscall.RLIMIT_FSIZE, &previous) {
        _, _ = os.Stdout.WriteString("probe-unavailable-getrlimit\n")

        return
    }

    if nil != syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 0, Max: previous.Max}) {
        _, _ = os.Stdout.WriteString("probe-unavailable-setrlimit\n")

        return
    }

    saveErr := storage.Save("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", map[string]any{"userId": "u-2"}, 0)

    _ = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &previous)

    if nil == saveErr {
        _, _ = os.Stdout.WriteString("probe-did-not-inject\n")

        return
    }

    after, readErr := os.ReadFile(path)
    if nil != readErr {
        _, _ = os.Stdout.WriteString("read-failed\n")

        return
    }

    if 0 == len(after) {
        _, _ = os.Stdout.WriteString("emptied\n")

        return
    }

    _, _ = os.Stdout.WriteString("intact\n")
}
