package debug

import (
    "bytes"
    "context"
    "encoding/json"
    "strings"
    "testing"

    melodycli "github.com/precision-soft/melody/v4/cli"
    "github.com/precision-soft/melody/v4/container"
    melodyversion "github.com/precision-soft/melody/v4/version"
)

func runVersionUnderRoot(t *testing.T, version string, arguments []string) (string, error) {
    t.Helper()

    buffer := &bytes.Buffer{}

    root := melodycli.NewRoot("app", "")
    root.SetVersion(version)
    root.SetWriter(buffer)
    root.SetErrorWriter(buffer)
    root.Register(&VersionCommand{}, newTestRuntime(container.NewContainer(container.ContainerConfig{})))

    runErr := root.Run(context.Background(), append([]string{"app", "debug:version"}, arguments...))

    return buffer.String(), runErr
}

func TestVersionCommand_TableFormat_PrintsTheThreeVersions(t *testing.T) {
    rendered, runErr := runVersionUnderRoot(t, "1.2.3", []string{})
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
        newTestRuntime(container.NewContainer(container.ContainerConfig{})),
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

func TestVersionCommand_JsonFormat_EachTreeNamesItsOwnVersion(t *testing.T) {
    for _, version := range []string{"1.0.0-first", "2.0.0-second", ""} {
        rendered, runErr := runVersionUnderRoot(t, version, []string{"--format=json"})
        if nil != runErr {
            t.Fatalf("expected no error, got %v", runErr)
        }

        envelope := struct {
            Meta struct {
                Version struct {
                    Application string `json:"application"`
                } `json:"version"`
            } `json:"meta"`
            Data map[string]string `json:"data"`
        }{}

        if decodeErr := json.Unmarshal([]byte(rendered), &envelope); nil != decodeErr {
            t.Fatalf("failed to decode the envelope: %v, rendered %q", decodeErr, rendered)
        }

        if version != envelope.Data["application"] || version != envelope.Meta.Version.Application {
            t.Fatalf("expected %q in the data and the meta, got %q and %q", version, envelope.Data["application"], envelope.Meta.Version.Application)
        }
    }
}

func TestVersionCommand_JsonFormat_CarriesTheThreeVersionsAsData(t *testing.T) {
    rendered, runErr := runVersionUnderRoot(t, "1.2.3", []string{"--format=json"})
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

/* the META header prints the version too, so the row itself is read */
func TestVersionCommand_TheDetailsRowCarriesTheDeclaredApplicationVersion(t *testing.T) {
    rendered, runErr := runVersionUnderRoot(t, "9.9.9-declared", []string{"--format=table", "--table-width=400"})
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

    if "9.9.9-declared" != applicationValue {
        t.Fatalf("expected the details row to carry the declared version, got %q in %q", applicationValue, rendered)
    }
}
