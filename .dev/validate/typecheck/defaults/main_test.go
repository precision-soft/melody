package main

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

/* writeFixtureRepository writes a repository root with a v4 module holding one declaration per reading the band has to make, so each assertion below names what it is about. */
func writeFixtureRepository(t *testing.T) string {
    t.Helper()

    rootDirectory := t.TempDir()
    fileMap := map[string]string{
        "v4/go.mod": "module github.com/precision-soft/melody/v4\n\ngo 1.25\n",
        "v4/bound/bound.go": `package bound

type Unit interface{ ~int | ~int64 }

type Limit[T Unit] struct{ value T }

type Count = Limit[int]

type Rule[T Unit] struct {
    Name              string
    Default           T
    DefaultUnlimited  bool
    Minimum           T
    Maximum           T
    UnlimitedAdmitted bool
}

type BackoffRule struct {
    Name           string
    InitialDefault int
}
`,
        "v4/config/contract/contract.go": `package contract

type EnvironmentKey struct {
    Key string
}
`,
        "v4/config/config.go": `package config

const SampleSizeKey = "MELODY_SAMPLE_SIZE"

const notAnEnvironmentName = "MELODY_NOT"
`,
        "v4/internal/warning.go": `package internal

const BootWarningNameSample = "sample.warning"
`,
        "v4/sample/sample.go": `package sample

import (
    "os"

    "github.com/precision-soft/melody/v4/bound"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
)

type SampleConfig struct {
    Size     bound.Count
    Disabled bool
    Name     string
}

type SampleResult struct {
    Disabled bool
}

const sampleFileMode os.FileMode = 0o640

var sizeRule = bound.Rule[int]{Name: "SampleConfig.Size", Default: 8, Minimum: 1}

var widthRule = bound.Rule[int]{Name: "SampleConfig.Width", Default: 8, Minimum: 1, Maximum: 64}

var openRule = bound.Rule[int]{Name: "SampleConfig.Open", Default: 8, UnlimitedAdmitted: true}

var backoffRule = bound.BackoffRule{Name: "SampleConfig.Backoff", InitialDefault: 1}

var moduleKey = configcontract.EnvironmentKey{Key: "MELODY_MODULE_KEY"}

func namedRule(name string) bound.Rule[int] {
    return bound.Rule[int]{Name: name, Default: 1}
}

func Write(path string) error {
    return os.MkdirAll(path, 0o750)
}

func Read(path string, mode os.FileMode) error {
    return os.MkdirAll(path, mode)
}

type registrar struct{}

func (instance registrar) DeclareBootWarning(name string, config int) {}

func Register(instance registrar) {
    instance.DeclareBootWarning("sample.moduleWarning", 0)
}
`,
    }

    for relativePath, content := range fileMap {
        absolutePath := filepath.Join(rootDirectory, relativePath)
        if makeErr := os.MkdirAll(filepath.Dir(absolutePath), 0o755); nil != makeErr {
            t.Fatalf("create %s: %v", relativePath, makeErr)
        }
        if writeErr := os.WriteFile(absolutePath, []byte(content), 0o644); nil != writeErr {
            t.Fatalf("write %s: %v", relativePath, writeErr)
        }
    }

    return rootDirectory
}

const tableHead = "| key | kind | protects against | default | door | reason | documented |\n|---|---|---|---|---|---|---|\n"

/* cleanRowList is a document every check of the fixture passes */
var cleanRowList = []string{
    "| `v4/sample.SampleConfig.Size` | limit | memory | 8 | `value:v4/sample.SampleConfig.Size` | class T | — |",
    "| `v4/sample.SampleConfig.Width` | limit | memory | 8 | `value:v4/sample.SampleConfig.Size` | a terminal is never wider | — |",
    "| `v4/sample.SampleConfig.Open` | limit | memory | 8 | `off:v4/sample.SampleConfig.Size` | — | — |",
    "| `v4/sample.SampleConfig.Backoff` | limit | a retry storm | 1 | `value:v4/sample.backoffRule` | class T | — |",
    "| `v4/sample.namedRule#1` | limit | memory | 1 | `value:v4/sample.namedRule`, `env:MELODY_SAMPLE_SIZE` | class T | — |",
    "| `v4/sample.Write#1` | mode | a world-readable directory | 0750 | none | the owner's | — |",
    "| `v4/sample.sampleFileMode` | mode | a world-readable file | 0640 | `env:MELODY_MODULE_KEY` | — | — |",
    "| `sample.warning` | warning | a silent misconfiguration | written | `env:MELODY_SAMPLE_SIZE` | — | — |",
    "| `sample.moduleWarning` | warning | a silent misconfiguration | written | none | written always | — |",
    "| `v4/sample.SampleConfig.Disabled` | protection | the sample | on | `off:v4/sample.SampleConfig.Disabled` | — | — |",
}

func runCheck(t *testing.T, rootDirectory string, rowList []string) []failure {
    t.Helper()

    documentPath := filepath.Join(rootDirectory, "DEFAULTS.md")
    if writeErr := os.WriteFile(documentPath, []byte("# Defaults\n\n"+tableHead+strings.Join(rowList, "\n")+"\n"), 0o644); nil != writeErr {
        t.Fatalf("write the document: %v", writeErr)
    }

    measured, measureErr := measure(rootDirectory, []string{"v4"}, append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod"))
    if nil != measureErr {
        t.Fatalf("measure: %v", measureErr)
    }

    rowListRead, parseFailureList, readErr := readDocument(documentPath)
    if nil != readErr {
        t.Fatalf("read: %v", readErr)
    }

    return append(parseFailureList, check(measured, rowListRead)...)
}

func replaceRow(rowList []string, prefix string, replacement string) []string {
    result := []string{}
    for _, current := range rowList {
        if true == strings.HasPrefix(current, prefix) {
            if "" != replacement {
                result = append(result, replacement)
            }

            continue
        }
        result = append(result, current)
    }

    return result
}

func expectFailure(t *testing.T, failureList []failure, kind string, key string, fragment string) {
    t.Helper()

    for _, current := range failureList {
        if kind == current.kind && key == current.key && true == strings.Contains(current.message, fragment) {
            return
        }
    }

    t.Fatalf("expected the failure %s ~ %s ~ …%s…, got %+v", kind, key, fragment, failureList)
}

func TestCheck_ACleanDocumentPasses(t *testing.T) {
    failureList := runCheck(t, writeFixtureRepository(t), cleanRowList)
    if 0 != len(failureList) {
        t.Fatalf("expected no failure, got %+v", failureList)
    }
}

func TestCheck_ARuleWithoutARowFails(t *testing.T) {
    rowList := replaceRow(cleanRowList, "| `v4/sample.SampleConfig.Backoff`", "")
    expectFailure(t, runCheck(t, writeFixtureRepository(t), rowList), kindLimit, "v4/sample.SampleConfig.Backoff", "has no row")
}

func TestCheck_ADeadRowFails(t *testing.T) {
    rowList := append(append([]string{}, cleanRowList...), "| `v4/sample.SampleConfig.Gone` | limit | memory | 8 | `value:v4/sample.SampleConfig.Size` | class T | — |")
    expectFailure(t, runCheck(t, writeFixtureRepository(t), rowList), kindLimit, "v4/sample.SampleConfig.Gone", "declares no such")
}

func TestCheck_ANoneDoorWithoutAReasonFails(t *testing.T) {
    rowList := replaceRow(cleanRowList, "| `v4/sample.Write#1`", "| `v4/sample.Write#1` | mode | a world-readable directory | 0750 | none | — | — |")
    expectFailure(t, runCheck(t, writeFixtureRepository(t), rowList), kindRow, "v4/sample.Write#1", "door is none and the reason is empty")
}

func TestCheck_AnEnvironmentDoorOnAProtectionFailsUnlessItHardens(t *testing.T) {
    rootDirectory := writeFixtureRepository(t)

    rowList := replaceRow(cleanRowList, "| `v4/sample.SampleConfig.Disabled`", "| `v4/sample.SampleConfig.Disabled` | protection | the sample | on | `off:v4/sample.SampleConfig.Disabled`, `env:MELODY_SAMPLE_SIZE` | — | — |")
    expectFailure(t, runCheck(t, rootDirectory, rowList), kindRow, "v4/sample.SampleConfig.Disabled", "a protection is turned off in Go only")

    hardened := replaceRow(cleanRowList, "| `v4/sample.SampleConfig.Disabled`", "| `v4/sample.SampleConfig.Disabled` | protection | the sample | on | `off:v4/sample.SampleConfig.Disabled`, `env:MELODY_SAMPLE_SIZE` | the key only hardens it | — |")
    if failureList := runCheck(t, rootDirectory, hardened); 0 != len(failureList) {
        t.Fatalf("expected a key that hardens to pass, got %+v", failureList)
    }
}

func TestCheck_AnEnvironmentDoorOnARefusalFails(t *testing.T) {
    rowList := append(append([]string{}, cleanRowList...), "| `v4/sample.Read` | refusal | a mode | refused | `env:MELODY_SAMPLE_SIZE` | refused | — |")
    expectFailure(t, runCheck(t, writeFixtureRepository(t), rowList), kindRow, "v4/sample.Read", "operational value only")
}

func TestCheck_AnEnvironmentDoorNamingNoKeyFails(t *testing.T) {
    rowList := replaceRow(cleanRowList, "| `sample.warning`", "| `sample.warning` | warning | a silent misconfiguration | written | `env:MELODY_NOT` | — | — |")
    expectFailure(t, runCheck(t, writeFixtureRepository(t), rowList), kindAnchor, "sample.warning", "no declared env key")
}

func TestCheck_AModeSiteWithoutARowFails(t *testing.T) {
    rootDirectory := writeFixtureRepository(t)

    failureList := runCheck(t, rootDirectory, replaceRow(cleanRowList, "| `v4/sample.Write#1`", ""))
    expectFailure(t, failureList, kindMode, "v4/sample.Write#1", "has no row")

    failureList = runCheck(t, rootDirectory, replaceRow(cleanRowList, "| `v4/sample.sampleFileMode`", ""))
    expectFailure(t, failureList, kindMode, "v4/sample.sampleFileMode", "has no row")
}

func TestCheck_AWarningCodeWithoutARowFails(t *testing.T) {
    rootDirectory := writeFixtureRepository(t)

    expectFailure(t, runCheck(t, rootDirectory, replaceRow(cleanRowList, "| `sample.warning`", "")), kindWarning, "sample.warning", "has no row")
    expectFailure(t, runCheck(t, rootDirectory, replaceRow(cleanRowList, "| `sample.moduleWarning`", "")), kindWarning, "sample.moduleWarning", "has no row")
}

func TestCheck_AnUnresolvableAnchorFails(t *testing.T) {
    rootDirectory := writeFixtureRepository(t)

    rowList := replaceRow(cleanRowList, "| `v4/sample.SampleConfig.Size`", "| `v4/sample.SampleConfig.Size` | limit | memory | 8 | `value:v4/sample.SampleConfig.Size`, `use:v4/sample.SampleConfig.Gone` | class T | — |")
    expectFailure(t, runCheck(t, rootDirectory, rowList), kindAnchor, "v4/sample.SampleConfig.Size", "names no declaration")

    handRow := append(append([]string{}, cleanRowList...), "| `v4/sample.Gone` | protection | — | on | none | by design | — |")
    expectFailure(t, runCheck(t, rootDirectory, handRow), kindAnchor, "v4/sample.Gone", "the key names no declaration")
}

func TestCheck_ADoorFieldNoRowNamesFails(t *testing.T) {
    rowList := replaceRow(cleanRowList, "| `v4/sample.SampleConfig.Disabled`", "")
    expectFailure(t, runCheck(t, writeFixtureRepository(t), rowList), kindDoor, "v4/sample.SampleConfig.Disabled", "no row names it as a door")
}

func TestCheck_ALimitRefusingUnlimitedNeedsAValueDoorAndAReason(t *testing.T) {
    rootDirectory := writeFixtureRepository(t)

    withoutReason := replaceRow(cleanRowList, "| `v4/sample.SampleConfig.Size`", "| `v4/sample.SampleConfig.Size` | limit | memory | 8 | `value:v4/sample.SampleConfig.Size` | — | — |")
    expectFailure(t, runCheck(t, rootDirectory, withoutReason), kindLimit, "v4/sample.SampleConfig.Size", "the reason is empty")

    offOnly := replaceRow(cleanRowList, "| `v4/sample.SampleConfig.Size`", "| `v4/sample.SampleConfig.Size` | limit | memory | 8 | `off:v4/sample.SampleConfig.Size` | class T | — |")
    expectFailure(t, runCheck(t, rootDirectory, offOnly), kindLimit, "v4/sample.SampleConfig.Size", "its door is value:")

    bareClamp := replaceRow(cleanRowList, "| `v4/sample.SampleConfig.Width`", "| `v4/sample.SampleConfig.Width` | limit | memory | 8 | `value:v4/sample.SampleConfig.Size` | class T | — |")
    expectFailure(t, runCheck(t, rootDirectory, bareClamp), kindLimit, "v4/sample.SampleConfig.Width", "clamps at a maximum")

    openUse := replaceRow(cleanRowList, "| `v4/sample.SampleConfig.Open`", "| `v4/sample.SampleConfig.Open` | limit | memory | 8 | `use:v4/sample.SampleConfig.Size` | — | — |")
    expectFailure(t, runCheck(t, rootDirectory, openUse), kindLimit, "v4/sample.SampleConfig.Open", "admits unlimited")
}

func TestCheck_ARepeatedKeyAndAMalformedRowFail(t *testing.T) {
    rootDirectory := writeFixtureRepository(t)

    repeated := append(append([]string{}, cleanRowList...), cleanRowList[0])
    expectFailure(t, runCheck(t, rootDirectory, repeated), kindRow, "v4/sample.SampleConfig.Size", "repeats the key")

    malformed := append(append([]string{}, cleanRowList...), "| `v4/sample.Read` | refusal | short |")
    failureList := runCheck(t, rootDirectory, malformed)
    expectFailure(t, failureList, kindRow, "line 15", "the table has 7")

    unknownKind := append(append([]string{}, cleanRowList...), "| `v4/sample.Read` | guess | — | — | none | by design | — |")
    expectFailure(t, runCheck(t, rootDirectory, unknownKind), kindRow, "v4/sample.Read", "is none of")

    unclassed := replaceRow(cleanRowList, "| `v4/sample.SampleConfig.Disabled`", "| `v4/sample.SampleConfig.Disabled` | protection | the sample | on | `v4/sample.SampleConfig.Disabled` | — | — |")
    expectFailure(t, runCheck(t, rootDirectory, unclassed), kindRow, "v4/sample.SampleConfig.Disabled", "has no class")
}

func TestMeasure_ReadsWhatTheBandEnumerates(t *testing.T) {
    measured, measureErr := measure(writeFixtureRepository(t), []string{"v4"}, append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod"))
    if nil != measureErr {
        t.Fatalf("measure: %v", measureErr)
    }

    for _, key := range []string{"v4/sample.SampleConfig.Size", "v4/sample.SampleConfig.Width", "v4/sample.SampleConfig.Open", "v4/sample.SampleConfig.Backoff", "v4/sample.namedRule#1", "v4/sample.Write#1", "v4/sample.sampleFileMode", "sample.warning", "sample.moduleWarning"} {
        if _, isItem := measured.itemMap[key]; false == isItem {
            t.Fatalf("expected the item %s, got %v", key, sortedKeyList(measured.itemMap))
        }
    }

    if 9 != len(measured.itemMap) {
        t.Fatalf("expected 9 items (the mode handed in as a variable is not one), got %v", sortedKeyList(measured.itemMap))
    }

    if true != measured.itemMap["v4/sample.SampleConfig.Open"].unlimitedAdmitted || true == measured.itemMap["v4/sample.SampleConfig.Size"].unlimitedAdmitted {
        t.Fatalf("expected only the open rule to admit unlimited")
    }
    if true != measured.itemMap["v4/sample.SampleConfig.Width"].hasMaximum {
        t.Fatalf("expected the width rule to clamp")
    }

    for _, key := range []string{"MELODY_SAMPLE_SIZE", "MELODY_MODULE_KEY"} {
        if _, isKey := measured.environmentMap[key]; false == isKey {
            t.Fatalf("expected the env key %s, got %v", key, sortedKeyList(measured.environmentMap))
        }
    }
    if _, isKey := measured.environmentMap["MELODY_NOT"]; true == isKey {
        t.Fatalf("a constant whose name does not end in Key is no env key")
    }

    if fieldList := sortedKeyList(measured.doorFieldMap); 2 != len(fieldList) || "v4/sample.SampleConfig.Disabled" != fieldList[0] || "v4/sample.SampleConfig.Size" != fieldList[1] {
        t.Fatalf("expected the two door fields of the configuration, not the result's, got %v", fieldList)
    }
}

func TestApplyBaseline_HoldsByKindAndKeyAndRefusesAStaleLine(t *testing.T) {
    failureList := []failure{
        {kind: kindLimit, key: "v4/sample.A", message: "the reason is empty"},
        {kind: kindRow, key: "v4/sample.B", message: "the door is none"},
    }
    baselineList := []baselineLine{
        {kind: kindLimit, key: "v4/sample.A", reason: "C2", lineNumber: 1},
        {kind: kindRow, key: "v4/sample.C", reason: "repaired", lineNumber: 2},
        {kind: kindLimit, key: "v4/sample.B", reason: "another kind", lineNumber: 3},
    }

    openList, staleList := applyBaseline(failureList, baselineList)
    if 1 != len(openList) || "v4/sample.B" != openList[0].key {
        t.Fatalf("expected only B open, got %+v", openList)
    }
    if 2 != len(staleList) || 2 != staleList[0].lineNumber || 3 != staleList[1].lineNumber {
        t.Fatalf("expected the lines 2 and 3 stale, got %+v", staleList)
    }
}

func TestReadBaseline_RefusesALineWithoutItsReason(t *testing.T) {
    baselinePath := filepath.Join(t.TempDir(), "defaults.baseline")
    if writeErr := os.WriteFile(baselinePath, []byte("# head\n\nlimit ~ v4/sample.A ~ C2\nrow ~ v4/sample.B ~ \n"), 0o644); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    if _, readErr := readBaseline(baselinePath); nil == readErr || false == strings.Contains(readErr.Error(), ":4:") {
        t.Fatalf("expected line 4 refused, got %v", readErr)
    }
}
