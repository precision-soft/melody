package main

import (
    "os"
    "strings"
    "testing"
)

/* stack.sh is bash dev tooling with no runtime harness in this repo, so these guards pin the invariants the
findings turned on directly in the script text: a removed diagnostic or a dropped cleanup step re-fails the
test, which is exactly the regression each fix prevents. */
func readStackScript(t *testing.T) string {
    t.Helper()

    content, readErr := os.ReadFile("stack.sh")
    if nil != readErr {
        t.Fatalf("read stack.sh: %v", readErr)
    }

    return string(content)
}

/* @info on a cold go build cache the holder outruns the marker-wait budget; the exclusive section must emit a
distinct holder-marker-timeout diagnostic and NOT launch the contender, so a build delay is never misreported
as a mutual-exclusion violation the lock never committed. */
func TestStackScript_ExclusiveMarkerTimeoutFailsLoudly(t *testing.T) {
    script := readStackScript(t)

    if false == strings.Contains(script, "holder_marker_timeout=1") {
        t.Fatal("the exclusive section must emit a distinct holder_marker_timeout diagnostic when the start marker never appears within the wait budget")
    }

    if false == strings.Contains(script, "never printed the start marker") {
        t.Fatal("the exclusive section must fail with a distinct 'holder never printed the start marker' message instead of the mutual-exclusion assertions")
    }

    /* the timeout branch must guard the exclusivity assertions, so an empty holder log does not trip them */
    timeoutGuardIndex := strings.Index(script, "grep -q 'holder_marker_timeout=1'")
    startedCountIndex := strings.Index(script, "STARTED_COUNT_INTEGER=")
    if -1 == timeoutGuardIndex || -1 == startedCountIndex {
        t.Fatal("could not locate the timeout guard and the started-count assertion")
    }
    if timeoutGuardIndex >= startedCountIndex {
        t.Fatal("the holder-marker-timeout guard must precede (and short-circuit) the mutual-exclusion assertions")
    }
}

/* @info a prior run killed before its EXIT trap ran leaves MELODY_PROCESS_ROLE=web in .env.local on the repo
bind mount; the default-role check must clear it first (mirroring the rm -f hygiene the other sections use) so
a stale role cannot poison the default-role assertion. */
func TestStackScript_ProcessRoleClearsStaleEnvLocalBeforeDefaultCheck(t *testing.T) {
    script := readStackScript(t)

    trapMarker := "trap restore_example_env_local EXIT"
    trapIndex := strings.Index(script, trapMarker)
    if -1 == trapIndex {
        t.Fatal("could not locate the process-role EXIT trap")
    }

    defaultCheckIndex := strings.Index(script, "DEFAULT_ROLE_STRING=")
    if -1 == defaultCheckIndex {
        t.Fatal("could not locate the default-role check")
    }

    /* the cleanup call has to sit strictly between arming the trap and the first default-role probe; before the
       fix this region held only blank lines, so the assertion is red on unpatched stack.sh */
    region := script[trapIndex+len(trapMarker) : defaultCheckIndex]
    if false == strings.Contains(region, "restore_example_env_local") {
        t.Fatal("the default-role check must clear a stale .env.local (call restore_example_env_local) before probing the role")
    }
}
