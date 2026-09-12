package debug

import (
    "encoding/json"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/container"
    melodyversion "github.com/precision-soft/melody/v3/version"
)

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

    if true == strings.Contains(rendered, "| application | "+melodyversion.BuildVersion()) {
        t.Fatalf("expected the application row to stop borrowing melody's version, got %q", rendered)
    }
}

/* the application row reads the process-wide declaration the composition root makes through output.SetApplicationVersion; an explicit value on the command still wins over it */
func TestVersionCommand_ReadsTheProcessWideApplicationVersion(t *testing.T) {
    output.SetApplicationVersion("7.7.7-declared")
    defer output.SetApplicationVersion("")

    rendered, runErr := runDebugCommand(
        &VersionCommand{},
        newTestRuntime(container.NewContainer()),
        []string{},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if false == strings.Contains(rendered, "7.7.7-declared") {
        t.Fatalf("expected the declared application version, got %q", rendered)
    }

    explicitRendered, explicitErr := runDebugCommand(
        &VersionCommand{ApplicationVersion: "8.8.8-explicit"},
        newTestRuntime(container.NewContainer()),
        []string{},
    )
    if nil != explicitErr {
        t.Fatalf("expected no error, got %v", explicitErr)
    }

    if false == strings.Contains(explicitRendered, "8.8.8-explicit") {
        t.Fatalf("expected the explicit value to win over the declaration, got %q", explicitRendered)
    }
}

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

/* the siblings above read the whole rendering, which the META header shadows: the header prints the application version off the same meta, so a details row reading the command's own field instead of the resolved chain still leaves the declared value in the output and every Contains assertion passes. This one reads the ROW, which is the only place the two answers differ. The frozen majors assert on the rendering alone, so the row is unpinned there too. */
func TestVersionCommand_TheDetailsRowCarriesTheResolvedApplicationVersion(t *testing.T) {
    output.SetApplicationVersion("9.9.9-process-wide")
    defer output.SetApplicationVersion("")

    rendered, runErr := runDebugCommand(
        &VersionCommand{},
        newTestRuntime(container.NewContainer()),
        []string{"--format=table", "--table-width=400"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    rows := debugTableBlockRow(rendered, "DETAILS")

    applicationValue := ""
    for _, row := range rows {
        if 2 <= len(row) && "application" == row[0] {
            applicationValue = row[1]
        }
    }

    if "9.9.9-process-wide" != applicationValue {
        t.Fatalf("expected the details row to carry the process-wide declaration, got %q in %q", applicationValue, rendered)
    }
}
