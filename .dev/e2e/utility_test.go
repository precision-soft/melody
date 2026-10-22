package main

import (
    "context"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "syscall"
    "testing"
    "time"
)

/* runValidationLockScript runs a bash script that sources the repository's utility.sh with the validation lock pointed at a file of the test's own, never at the lock of the run that may be executing this test: that run holds it from the host, where its marker does not reach, and taking it here would wait on the run for good. The script answers within the bound or the run is killed and reported as such. */
func runValidationLockScript(t *testing.T, lockPath string, script string, bound time.Duration) (string, bool) {
    t.Helper()

    utilityPath, absoluteErr := filepath.Abs("../utility.sh")
    if nil != absoluteErr {
        t.Fatalf("resolve utility.sh: %v", absoluteErr)
    }

    commandContext, cancel := context.WithTimeout(context.Background(), bound)
    defer cancel()

    /* the script runs in a process group of its own, killed whole at the bound: killing the shell alone would release the lock it holds and hand it to a band still waiting under it, which would then finish as if it had never waited */
    command := exec.CommandContext(commandContext, "bash", "-c", script)
    command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
    command.Cancel = func() error {
        return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
    }
    command.Env = append(command.Environ(), "MELODY_TEST_UTILITY="+utilityPath, "MELODY_TEST_LOCK="+lockPath, "MELODY_VALIDATION_LOCK_HELD=")

    output, _ := command.CombinedOutput()

    return string(output), nil == commandContext.Err()
}

/* the prologue every script below opens with: utility.sh sourced, the lock moved to the test's file. A band is never the last command of its script: bash runs the last command of a -c string in place of the shell, so that band would BE the run, whose descriptor it closes when it opens the file again, and the lock would be released rather than contended */
const validationLockPrologue = `. "${MELODY_TEST_UTILITY}"; VALIDATION_LOCK_FILE_PATH="${MELODY_TEST_LOCK}"; `

/* a band all.sh starts finds the marker its parent exported and returns at once: the lock is held on the descriptor it inherited */
func TestValidationLock_ABandStartedByARunTakesNothingAndReturns(t *testing.T) {
    lockPath := filepath.Join(t.TempDir(), "validate.lock")

    output, returned := runValidationLockScript(t, lockPath, validationLockPrologue+`acquire_validation_lock; bash -c '. "${MELODY_TEST_UTILITY}"; VALIDATION_LOCK_FILE_PATH="${MELODY_TEST_LOCK}"; acquire_validation_lock; echo band-done'; echo run-done`, 10*time.Second)

    if false == returned || false == strings.Contains(output, "band-done") {
        t.Fatalf("a band started under a run did not return; it waits on the lock its own parent holds: %q", output)
    }
}

/* the control that makes the marker load-bearing: without it, the band started under a run opens the file again, a second open file description, and waits on its own parent */
func TestValidationLock_ABandStartedByARunWithoutTheMarkerWaitsOnItsParent(t *testing.T) {
    lockPath := filepath.Join(t.TempDir(), "validate.lock")

    output, returned := runValidationLockScript(t, lockPath, validationLockPrologue+`acquire_validation_lock; MELODY_VALIDATION_LOCK_HELD= bash -c '. "${MELODY_TEST_UTILITY}"; VALIDATION_LOCK_FILE_PATH="${MELODY_TEST_LOCK}"; acquire_validation_lock; echo band-done'; echo run-done`, 2*time.Second)

    if true == returned || true == strings.Contains(output, "band-done") || false == strings.Contains(output, "waiting for it to finish") {
        t.Fatalf("a band without the marker did not wait on the lock its parent holds, so nothing here proves the marker is what returns it: %q", output)
    }
}

/* a band started by hand beside a run, with no marker of its own, waits for that run to finish */
func TestValidationLock_ARunStartedBesideAnotherWaitsForIt(t *testing.T) {
    lockPath := filepath.Join(t.TempDir(), "validate.lock")

    holderDone := make(chan struct{})
    go func() {
        defer close(holderDone)
        runValidationLockScript(t, lockPath, validationLockPrologue+`acquire_validation_lock; : > "${MELODY_TEST_LOCK}.held"; sleep 1`, 10*time.Second)
    }()

    /* the second run starts only once the first holds the lock, which it marks with a file written after taking it */
    deadline := time.Now().Add(5 * time.Second)
    for {
        if _, statErr := os.Stat(lockPath + ".held"); nil == statErr {
            break
        }

        if true == time.Now().After(deadline) {
            t.Fatal("the first run never took the lock; there is nothing for the second to wait on")
        }

        time.Sleep(10 * time.Millisecond)
    }

    started := time.Now()
    output, returned := runValidationLockScript(t, lockPath, validationLockPrologue+`acquire_validation_lock; echo second-done`, 10*time.Second)
    waited := time.Since(started)

    <-holderDone

    if false == returned || false == strings.Contains(output, "second-done") {
        t.Fatalf("the second run never took the lock after the first finished: %q", output)
    }

    if waited < 500*time.Millisecond {
        t.Fatalf("the second run took the lock after %s while the first held it for a second; it did not wait", waited)
    }
}
