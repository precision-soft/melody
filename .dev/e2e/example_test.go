package main

import (
    "net/http"
    "os"
    "strings"
    "testing"
)

/* an UNSET MELODY_E2E_MAJORS must mean every major: the project's convention is that a default run is the
full run, and a default that quietly resolved to v3 alone is exactly the gap these sections exist to close. */
func TestExampleMajorList_UnsetCoversEveryMajor(t *testing.T) {
    /* Setenv arms the cleanup that restores whatever the run was started with; Unsetenv then exercises the
       LookupEnv fallback the default depends on */
    t.Setenv(exampleMajorListVariable, exampleMajorListDefault)
    if unsetErr := os.Unsetenv(exampleMajorListVariable); nil != unsetErr {
        t.Fatalf("unset %s: %v", exampleMajorListVariable, unsetErr)
    }

    majors := exampleMajorList()
    if len(exampleMajorCatalog) != len(majors) {
        t.Fatalf("an unset %s must cover all %d majors, got %d", exampleMajorListVariable, len(exampleMajorCatalog), len(majors))
    }

    for index, major := range majors {
        if exampleMajorCatalog[index].number != major.number {
            t.Fatalf("major %d of the default list is %d, wanted %d", index, major.number, exampleMajorCatalog[index].number)
        }
    }
}

/* an EMPTY value is the opt-out, the same clear-to-skip contract the backend variables follow; the caller
announces the skip, so the list itself has to come back empty rather than fall back to the default. */
func TestExampleMajorList_EmptyValueSelectsNothing(t *testing.T) {
    t.Setenv(exampleMajorListVariable, "")

    if majors := exampleMajorList(); 0 != len(majors) {
        t.Fatalf("an empty %s must select no major, got %d", exampleMajorListVariable, len(majors))
    }
}

func TestExampleMajorList_AcceptsSpaceCommaAndVPrefixedEntries(t *testing.T) {
    t.Setenv(exampleMajorListVariable, " v1,3 ")

    majors := exampleMajorList()
    if 2 != len(majors) {
        t.Fatalf("wanted 2 majors from %q, got %d", " v1,3 ", len(majors))
    }
    if 1 != majors[0].number || 3 != majors[1].number {
        t.Fatalf("wanted majors 1 and 3, got %d and %d", majors[0].number, majors[1].number)
    }
}

/* the ports have to stay distinct from each other, from the :8080 the dev container supervises and from the
:18080 stack.sh's signal check uses, or two applications fight for one socket and the section that loses reports a
boot failure that has nothing to do with the code under test. */
func TestExampleMajorCatalog_PortsAreDistinctAndReserved(t *testing.T) {
    seen := map[int]bool{
        8080:  true,
        18080: true,
    }

    for _, major := range exampleMajorCatalog {
        if true == seen[major.port] {
            t.Fatalf("major %s reuses port %d", major.label, major.port)
        }

        seen[major.port] = true
    }
}

/* the command prints its boot log to stdout before the file logger exists, and those lines are json objects
of their own; picking the FIRST decodable object would hand a log line to the decoder instead of the envelope,
and under --format=json the envelope is a single line exactly like they are. */
func TestExampleJsonDocument_SkipsTheBootLogLines(t *testing.T) {
    output := strings.Join(
        []string{
            `{"message":"configuration defaults applied","level":"info"}`,
            `{"message":"configuration validated","level":"info"}`,
            `{"meta":{"command":"debug:router"},"data":{"total":23},"error":null}`,
        },
        "\n",
    )

    document := exampleJsonDocument(output)
    if false == strings.HasPrefix(document, `{"meta"`) {
        t.Fatalf("the envelope is the line carrying the meta key, got %q", document)
    }
    if true == strings.Contains(document, "configuration validated") {
        t.Fatalf("the boot log lines must not be part of the decoded document, got %q", document)
    }
}

/* the pretty document is still read, so a run driven with --format=json-pretty by hand reports what it found
rather than a decode failure that reads like a broken command */
func TestExampleJsonDocument_StillReadsAPrettyPrintedEnvelope(t *testing.T) {
    output := strings.Join(
        []string{
            `{"message":"configuration validated","level":"info"}`,
            "{",
            `  "data": {"total": 23}`,
            "}",
        },
        "\n",
    )

    document := exampleJsonDocument(output)
    if false == strings.HasPrefix(document, "{\n  \"data\"") {
        t.Fatalf("the envelope must start at the pretty-printed opening brace, got %q", document)
    }
    if true == strings.Contains(document, "configuration validated") {
        t.Fatalf("the boot log lines must not be part of the decoded document, got %q", document)
    }
}

func TestExampleReportsPositiveCount(t *testing.T) {
    if false == exampleReportsPositiveCount("routes: 23\nservices: 23\n", "routes:") {
        t.Fatal("a positive count must be recognised")
    }
    if true == exampleReportsPositiveCount("routes: 0\n", "routes:") {
        t.Fatal("a zero count means an empty application and must NOT be recognised")
    }
    if true == exampleReportsPositiveCount("services: 23\n", "routes:") {
        t.Fatal("a missing label must not be satisfied by another one")
    }
}

/* the assertion reports the attribute by name, so an unset SameSite must never be printed as if the cookie
carried one. */
func TestExampleSameSiteName(t *testing.T) {
    for sameSite, expected := range map[http.SameSite]string{
        http.SameSiteLaxMode:     "Lax",
        http.SameSiteStrictMode:  "Strict",
        http.SameSiteNoneMode:    "None",
        http.SameSiteDefaultMode: "Default",
    } {
        if name := exampleSameSiteName(sameSite); expected != name {
            t.Fatalf("SameSite %d rendered as %q, wanted %q", sameSite, name, expected)
        }
    }
}

/* fail() exits through os.Exit, which runs no deferred function: without this hook a started example
process outlives the failing run and holds its port, so the NEXT run reports an occupied port instead of the
failure that actually happened. */
func TestPushFailureCleanup_RunsRegisteredTeardownAndSkipsPopped(t *testing.T) {
    failureCleanupList = nil

    keptRan := false
    poppedRan := false

    _ = pushFailureCleanup(func() {
        keptRan = true
    })

    pop := pushFailureCleanup(func() {
        poppedRan = true
    })
    pop()

    runFailureCleanup()

    if false == keptRan {
        t.Fatal("a registered teardown must run before the process exits")
    }
    if true == poppedRan {
        t.Fatal("a teardown whose section finished cleanly must not run for a later, unrelated failure")
    }
    if 0 != len(failureCleanupList) {
        t.Fatalf("the cleanup list must be emptied after it ran, holds %d", len(failureCleanupList))
    }
}
