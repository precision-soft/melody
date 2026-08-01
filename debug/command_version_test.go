package debug

import (
    "encoding/json"
    "strings"
    "testing"

    "github.com/precision-soft/melody/container"
    melodyversion "github.com/precision-soft/melody/version"
)

/* @info the whole command had never been run by a test, in either format. It is the one an operator runs first when a deployment misbehaves, and the three versions it prints — the application's, the framework's and the runtime's — are the answer to "what is actually installed there" */
func TestVersionCommand_TableFormat_PrintsTheThreeVersions(t *testing.T) {
    rendered, runErr := runDebugCommand(
        &VersionCommand{ApplicationVersion: "1.2.3"},
        newTestRuntime(container.NewContainer()),
        []string{},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(rendered, "VERSIONS") {
        t.Fatalf("expected the summary line, got %q", rendered)
    }

    if false == strings.Contains(rendered, "1.2.3") {
        t.Fatalf("expected the application version, got %q", rendered)
    }

    if false == strings.Contains(rendered, melodyversion.BuildVersion()) {
        t.Fatalf("expected the melody version, got %q", rendered)
    }

    if false == strings.Contains(rendered, "go") {
        t.Fatalf("expected the go runtime row, got %q", rendered)
    }
}

/* @info an application that never called SetApplicationVersion is the ordinary case for a fresh deployment, and the row must say so rather than printing an empty cell that reads as "no application" */
func TestVersionCommand_TableFormat_NamesAnUnknownApplicationVersion(t *testing.T) {
    rendered, runErr := runDebugCommand(
        &VersionCommand{},
        newTestRuntime(container.NewContainer()),
        []string{},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(rendered, "<unknown>") {
        t.Fatalf("expected the unknown placeholder, got %q", rendered)
    }
}

/* @info the json format is what a deployment pipeline reads, so the three versions must travel as data rather than only as a rendered table */
func TestVersionCommand_JsonFormat_CarriesTheThreeVersionsAsData(t *testing.T) {
    rendered, runErr := runDebugCommand(
        &VersionCommand{ApplicationVersion: "1.2.3"},
        newTestRuntime(container.NewContainer()),
        []string{"--format=json"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := struct {
        Data map[string]string `json:"data"`
    }{}

    if decodeErr := json.Unmarshal([]byte(rendered), &envelope); nil != decodeErr {
        t.Fatalf("failed to decode the envelope: %v, rendered %q", decodeErr, rendered)
    }

    if "1.2.3" != envelope.Data["application"] {
        t.Fatalf("expected the application version, got %q", envelope.Data["application"])
    }

    if melodyversion.BuildVersion() != envelope.Data["melody"] {
        t.Fatalf("expected the melody version, got %q", envelope.Data["melody"])
    }

    if "" == envelope.Data["go"] {
        t.Fatalf("expected the go runtime version, got %q", envelope.Data["go"])
    }
}

func TestVersionCommand_NameAndDescriptionAndFlags(t *testing.T) {
    command := &VersionCommand{}

    if "debug:version" != command.Name() {
        t.Fatalf("unexpected name %q", command.Name())
    }

    if "" == command.Description() {
        t.Fatalf("expected a description")
    }

    if 0 == len(command.Flags()) {
        t.Fatalf("expected the shared debug flags")
    }
}
