package version

import (
    "strings"
    "testing"
)

/* @info the build version is what every banner, every debug:version report and every user agent this framework sends is built from, and nothing had ever read it: the accessor could have returned the empty string for the whole life of the package without a test noticing, and a release would have gone out announcing nothing. The variable behind it is replaced at link time with -ldflags, so the test asserts the SHAPE the accessor must keep answering rather than a literal that would have to be edited at every bump. */
func TestBuildVersion_AnswersTheLinkedVersion(t *testing.T) {
    buildVersionValue := BuildVersion()

    if "" == buildVersionValue {
        t.Fatalf("expected a build version to be reported")
    }

    if false == strings.HasPrefix(buildVersionValue, "v") {
        t.Fatalf("expected the version to keep its leading v, got %q", buildVersionValue)
    }

    if 2 != strings.Count(buildVersionValue, ".") {
        t.Fatalf("expected a three-part semantic version, got %q", buildVersionValue)
    }
}

/* @info the accessor has to read the variable rather than a copy taken at initialisation, or the -ldflags override the release procedure depends on would be written into a value nothing serves. */
func TestBuildVersion_ReadsTheVariableTheLinkerReplaces(t *testing.T) {
    original := buildVersion
    defer func() {
        buildVersion = original
    }()

    buildVersion = "v9.9.9"

    if "v9.9.9" != BuildVersion() {
        t.Fatalf("expected the accessor to read the replaced variable, got %q", BuildVersion())
    }
}
