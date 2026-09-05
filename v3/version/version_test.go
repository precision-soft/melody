package version

import (
    "strings"
    "testing"
)

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
