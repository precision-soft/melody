package wiring

import (
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
)

func scanSource(t *testing.T, files map[string]string) *ScanResult {
    t.Helper()

    projectDirectory := t.TempDir()
    for relativePath, content := range files {
        writeFixtureFile(t, projectDirectory, relativePath, content)
    }

    scanResult, scanErr := Scan(ScanRequest{ProjectDirectory: projectDirectory, PackageBinding: NewBindSet().Package("github.com/acme/app/domain", "domain")})
    if nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    return scanResult
}

func resolveLifetime(t *testing.T, constructor *Constructor) error {
    t.Helper()

    bindSet := NewBindSet()
    _, _, resolveErr := resolveArguments(constructor, bindSet.Package("github.com/acme/app/domain", "domain"), &GenerateRequest{BindSet: bindSet}, make(map[string]bool), &GenerateReport{GlobalBindReach: make(map[string][]string)})

    return resolveErr
}

func TestScan_MarksAContextParameterByItsImportPath(t *testing.T) {
    scanResult := scanSource(t, map[string]string{
        "domain/service.go": `package domain

import (
    stdcontext "context"

    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/acme/app/context"
)

func NewAliased(ctx stdcontext.Context, runtimeInstance melodycontainercontract.Runtime) *Aliased {
    return &Aliased{}
}

type Aliased struct{}

func NewForeign(ctx context.Context) *Foreign {
    return &Foreign{}
}

type Foreign struct{}
`,
    })

    aliased := constructorByName(scanResult, "NewAliased")
    if nil == aliased || false == aliased.Arguments[0].IsContext || true == aliased.Arguments[0].IsRuntime || false == aliased.Arguments[1].IsRuntime || true == aliased.Arguments[1].IsContext {
        t.Fatalf("expected the aliased stdlib context and the aliased runtime marked by import path, got %+v", aliased)
    }

    foreign := constructorByName(scanResult, "NewForeign")
    if nil == foreign || true == foreign.Arguments[0].IsContext {
        t.Fatalf("expected a foreign package called context not taken for the standard one, got %+v", foreign)
    }
}

func TestScan_CollectsTheFieldsOfAReturnedStructOfTheScannedPackage(t *testing.T) {
    scanResult := scanSource(t, map[string]string{
        "domain/worker.go": `package domain

import "context"

func NewWorker(ctx context.Context) *Worker {
    return &Worker{}
}

func NewHandle() Handle {
    return nil
}

type Handle interface{}
`,
        "domain/worker_type.go": `package domain

import (
    "context"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type Worker struct {
    context.Context
    cancel  *context.CancelFunc
    runtime containercontract.Runtime
    name    string
}
`,
    })

    worker := constructorByName(scanResult, "NewWorker")
    if nil == worker || 4 != len(worker.ReturnFields) {
        t.Fatalf("expected the four fields of a struct declared in another file, got %+v", worker)
    }

    names := make([]string, 0, len(worker.ReturnFields))
    for _, field := range worker.ReturnFields {
        names = append(names, field.Name+" "+field.Type.Expression)
    }

    if "Context context.Context|cancel *context.CancelFunc|runtime containercontract.Runtime|name string" != strings.Join(names, "|") {
        t.Fatalf("expected the embedded, pointer and plain fields by name and type, got %v", names)
    }

    if handle := constructorByName(scanResult, "NewHandle"); nil == handle || nil != handle.ReturnFields {
        t.Fatalf("expected an interface return to carry no fields, got %+v", handle)
    }
}

func TestRender_HandsAContextParameterTheResolutionContext(t *testing.T) {
    golden, readErr := os.ReadFile(filepath.FromSlash(fixtureGoldenFile))
    if nil != readErr {
        t.Fatalf("read the golden file: %v", readErr)
    }

    source := string(golden)
    if false == strings.Contains(source, "ctx := resolver.Runtime().Context()") || false == strings.Contains(source, "runtimeInstance := resolver.Runtime()") {
        t.Fatal("expected the context and the runtime handed from the resolution")
    }

    if true == strings.Contains(source, "FromResolverByType[context.Context]") || true == strings.Contains(source, "FromResolverByType[containercontract.Runtime]") || true == strings.Contains(source, "FromResolverByType[melodycontainercontract.Runtime]") {
        t.Fatal("expected no by-type lookup of the context or the runtime")
    }
}

func TestResolveArguments_RefusesAPointerToAContext(t *testing.T) {
    resolveErr := resolveLifetime(t, &Constructor{
        Name:      "NewHolder",
        File:      "holder.go",
        Line:      3,
        Arguments: []*Argument{{Name: "ctx", Type: &TypeReference{Expression: "*context.Context", Qualifier: "context", ImportPath: "context", IsPointer: true}}},
    })

    if nil == resolveErr || "a pointer to a context cannot be resolved: take context.Context" != exception.FromError(resolveErr).Message() || "ctx" != exception.FromError(resolveErr).Context()["argument"] {
        t.Fatalf("expected the pointer refused naming the argument, got %v", resolveErr)
    }
}

var contextArgument = &Argument{Name: "ctx", Type: &TypeReference{Expression: "context.Context", Qualifier: "context", ImportPath: "context"}, IsContext: true}

func keptField(expression string, importPath string) []*FieldReference {
    return []*FieldReference{{Name: "name", Type: &TypeReference{Expression: "string"}}, {Name: "kept", Type: &TypeReference{Expression: expression, ImportPath: importPath}}}
}

func TestGenerate_RefusesAProcessConstructorKeepingTheResolutionContext(t *testing.T) {
    runtimeArgument := &Argument{Name: "runtimeInstance", Type: &TypeReference{Expression: "containercontract.Runtime", ImportPath: containerContractImportPath}, IsRuntime: true}

    for _, testCase := range []struct {
        argument *Argument
        fields   []*FieldReference
    }{
        {contextArgument, keptField("context.Context", "context")},
        {contextArgument, keptField("*context.CancelFunc", "context")},
        {contextArgument, keptField("containercontract.Runtime", containerContractImportPath)},
        {runtimeArgument, keptField("context.Context", "context")},
    } {
        resolveErr := resolveLifetime(t, &Constructor{Name: "NewWorker", File: "worker.go", Line: 7, Arguments: []*Argument{testCase.argument}, ReturnFields: testCase.fields})

        refusal := exception.FromError(resolveErr)
        if nil == resolveErr || false == strings.HasPrefix(refusal.Message(), "a process service cannot keep the context of the resolution that built it") || "kept" != refusal.Context()["field"] || "NewWorker" != refusal.Context()["constructor"] || "worker.go" != refusal.Context()["file"] || 7 != refusal.Context()["line"] || "//melody:keeps-context" != refusal.Context()["door"] {
            t.Fatalf("%s: expected the kept resolution refused naming the constructor, the field and the door, got %v", testCase.fields[1].Type.Expression, resolveErr)
        }
    }
}

func TestGenerate_AdmitsAScopedConstructorKeepingTheResolutionContext(t *testing.T) {
    if resolveErr := resolveLifetime(t, &Constructor{Name: "NewWorker", IsScoped: true, Arguments: []*Argument{contextArgument}, ReturnFields: keptField("context.Context", "context")}); nil != resolveErr {
        t.Fatalf("expected a scoped constructor admitted, got %v", resolveErr)
    }
}

func TestGenerate_AdmitsAProcessConstructorUsingTheContextWithoutKeepingIt(t *testing.T) {
    if resolveErr := resolveLifetime(t, &Constructor{Name: "NewWarm", Arguments: []*Argument{contextArgument}, ReturnFields: keptField("string", "")}); nil != resolveErr {
        t.Fatalf("expected a context read while building admitted, got %v", resolveErr)
    }

    if resolveErr := resolveLifetime(t, &Constructor{Name: "NewOwner", ReturnFields: keptField("context.Context", "context")}); nil != resolveErr {
        t.Fatalf("expected a struct keeping a context it was not handed admitted, got %v", resolveErr)
    }
}

var requestLifetimeArguments = []*TypeReference{
    {Expression: "containercontract.Scope", ImportPath: "github.com/precision-soft/melody/v4/container/contract"},
    {Expression: "httpcontract.Request", ImportPath: "github.com/precision-soft/melody/v4/http/contract"},
    {Expression: "httpcontract.RequestContext", ImportPath: "github.com/precision-soft/melody/v4/http/contract"},
    {Expression: "httpcontract.Response", ImportPath: "github.com/precision-soft/melody/v4/http/contract"},
    {Expression: "securitycontract.Token", ImportPath: "github.com/precision-soft/melody/v4/security/contract"},
    {Expression: "sessioncontract.Session", ImportPath: "github.com/precision-soft/melody/v4/session/contract"},
}

func TestGenerate_RefusesAProcessConstructorTakingARequestLifetimeValue(t *testing.T) {
    for _, argumentType := range requestLifetimeArguments {
        resolveErr := resolveLifetime(t, &Constructor{Name: "NewHolder", Arguments: []*Argument{{Name: "value", Type: argumentType}}})

        if nil == resolveErr || false == strings.HasPrefix(exception.FromError(resolveErr).Message(), "a process service cannot be built from a value of one request") || "value" != exception.FromError(resolveErr).Context()["argument"] {
            t.Fatalf("%s: expected the request value refused naming the argument, got %v", argumentType.Expression, resolveErr)
        }
    }
}

func TestGenerate_AdmitsAScopedConstructorTakingARequestLifetimeValue(t *testing.T) {
    for _, argumentType := range requestLifetimeArguments {
        if resolveErr := resolveLifetime(t, &Constructor{Name: "NewHolder", IsScoped: true, Arguments: []*Argument{{Name: "value", Type: argumentType}}}); nil != resolveErr {
            t.Fatalf("%s: expected a scoped constructor admitted, got %v", argumentType.Expression, resolveErr)
        }
    }
}

func TestRequestLifetimeTypes_ListsEveryRequestValueOfTheFourthMajor(t *testing.T) {
    if len(requestLifetimeArguments) != len(requestLifetimeTypes) {
        t.Fatalf("expected the table of the refusal pin to cover every listed type, got %d of %d", len(requestLifetimeArguments), len(requestLifetimeTypes))
    }

    for key := range requestLifetimeTypes {
        separatorIndex := strings.LastIndex(key, ".")
        importPath, name := key[:separatorIndex], key[separatorIndex+1:]

        directory := filepath.Join("..", strings.TrimPrefix(importPath, "github.com/precision-soft/melody/v4/"))
        entries, readErr := os.ReadDir(directory)
        if nil != readErr {
            t.Fatalf("%s: the package of a listed type does not exist: %v", key, readErr)
        }

        declared := false
        for _, entry := range entries {
            if true == entry.IsDir() || false == strings.HasSuffix(entry.Name(), ".go") {
                continue
            }

            content, _ := os.ReadFile(filepath.Join(directory, entry.Name()))
            if true == strings.Contains(string(content), "\ntype "+name+" interface") {
                declared = true
            }
        }

        if false == declared {
            t.Fatalf("%s: the listed type is not declared, so a rename orphaned the row", key)
        }
    }
}

func TestGenerate_TheOffDoorAdmitsAKeptContext(t *testing.T) {
    if resolveErr := resolveLifetime(t, &Constructor{Name: "NewWorker", KeepsContext: true, Arguments: []*Argument{contextArgument}, ReturnFields: keptField("context.Context", "context")}); nil != resolveErr {
        t.Fatalf("expected the door to admit a kept context, got %v", resolveErr)
    }

    for name, constructor := range map[string]*Constructor{
        "nothing kept": {Name: "NewWarm", KeepsContext: true, Arguments: []*Argument{contextArgument}, ReturnFields: keptField("string", "")},
        "scoped":       {Name: "NewWorker", KeepsContext: true, IsScoped: true, Arguments: []*Argument{contextArgument}, ReturnFields: keptField("context.Context", "context")},
    } {
        resolveErr := resolveLifetime(t, constructor)
        if nil == resolveErr || false == strings.Contains(exception.FromError(resolveErr).Message(), "admits nothing: remove it") {
            t.Fatalf("%s: expected a door that admits nothing refused, got %v", name, resolveErr)
        }
    }
}

func TestScan_ReadsTheKeepsContextDirective(t *testing.T) {
    scanResult := scanSource(t, map[string]string{
        "domain/worker.go": `package domain

import "context"

//melody:keeps-context
func NewWorker(ctx context.Context) *Worker {
    return &Worker{ctx: ctx}
}

type Worker struct {
    ctx context.Context
}
`,
    })

    if worker := constructorByName(scanResult, "NewWorker"); nil == worker || false == worker.KeepsContext {
        t.Fatalf("expected the directive read, got %+v", worker)
    }
}
