package application

import (
    "context"
    "go/ast"
    "go/parser"
    "go/token"
    "slices"
    "strconv"
    "strings"
    "testing"

    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

func declarationOf(t *testing.T, declarations []frameworkEventDeclaration, eventName string) frameworkEventDeclaration {
    t.Helper()

    for _, declaration := range declarations {
        if eventName == declaration.eventName {
            return declaration
        }
    }

    t.Fatalf("expected %s declared", eventName)

    return frameworkEventDeclaration{}
}

func TestResolveFrameworkEventConfigs_DeclaresTheTwelveFrameworkEvents(t *testing.T) {
    declarations, resolveErr := resolveFrameworkEventConfigs(nil)
    if nil != resolveErr || 12 != len(declarations) {
        t.Fatalf("expected twelve declarations, got %d: %v", len(declarations), resolveErr)
    }

    for _, declaration := range declarations {
        expected := ""
        switch declaration.eventName {
        case kernelcontract.EventKernelResponse:
            expected = kernelcontract.ListenerTagCors
        case kernelcontract.EventKernelTerminate:
            expected = kernelcontract.ListenerTagAccessLog
        }

        if expected != strings.Join(declaration.config.RunAfterFailureTags, ",") {
            t.Fatalf("%s: expected tags %q, got %v", declaration.eventName, expected, declaration.config.RunAfterFailureTags)
        }
    }
}

func TestResolveFrameworkEventConfigs_MergesTheApplicationsTagsAfterTheFrameworks(t *testing.T) {
    applicationTags := []string{"audit", "cache"}

    declarations, resolveErr := resolveFrameworkEventConfigs(map[string]eventcontract.EventConfig{
        kernelcontract.EventKernelResponse: {RunAfterFailureTags: applicationTags},
        kernelcontract.EventKernelRequest:  {RunAfterFailureTags: []string{"audit"}},
    })
    if nil != resolveErr {
        t.Fatalf("unexpected refusal: %v", resolveErr)
    }

    merged := declarationOf(t, declarations, kernelcontract.EventKernelResponse).config.RunAfterFailureTags
    if "melody.cors,audit,cache" != strings.Join(merged, ",") {
        t.Fatalf("expected the framework's tags first, got %v", merged)
    }

    merged[1] = "changed"
    if "audit" != applicationTags[0] {
        t.Fatalf("expected the caller's slice not aliased")
    }

    if "audit" != strings.Join(declarationOf(t, declarations, kernelcontract.EventKernelRequest).config.RunAfterFailureTags, ",") {
        t.Fatalf("expected an event without framework tags to carry the application's alone")
    }
}

func eventsRefusalOf(t *testing.T, events map[string]eventcontract.EventConfig) *exception.Error {
    t.Helper()

    _, resolveErr := resolveFrameworkEventConfigs(events)

    refusal, isRefusal := resolveErr.(*exception.Error)
    if false == isRefusal {
        t.Fatalf("expected a refusal, got %v", resolveErr)
    }

    return refusal
}

func TestResolveFrameworkEventConfigs_RefusesAnEventTheFrameworkDoesNotOwn(t *testing.T) {
    refusal := eventsRefusalOf(t, map[string]eventcontract.EventConfig{"user.deleted": {}, "app.created": {}})

    if "ApplicationConfig.Events names an event the framework does not own; an application event is declared through EventDispatcher.DeclareEvent" != refusal.Message() || "app.created" != refusal.Context()["eventName"] {
        t.Fatalf("expected the sorted first unknown event named, got %q %v", refusal.Message(), refusal.Context())
    }
}

func TestResolveFrameworkEventConfigs_RefusesATagInTheFrameworkNamespace(t *testing.T) {
    for eventName, tag := range map[string]string{kernelcontract.EventKernelResponse: kernelcontract.ListenerTagCors, kernelcontract.EventKernelRequest: "melody.anything"} {
        refusal := eventsRefusalOf(t, map[string]eventcontract.EventConfig{eventName: {RunAfterFailureTags: []string{"audit", tag}}})

        if "ApplicationConfig.Events carries a tag in the melody. namespace, which the framework declares itself" != refusal.Message() || tag != refusal.Context()["tag"] || `ApplicationConfig.Events["`+eventName+`"].RunAfterFailureTags` != refusal.Context()["field"] {
            t.Fatalf("%s: expected the reserved tag refused naming the field, got %q %v", eventName, refusal.Message(), refusal.Context())
        }
    }
}

func TestResolveFrameworkEventConfigs_RefusesAMalformedTagNamingTheField(t *testing.T) {
    for _, tags := range [][]string{{""}, {"a b"}, {"audit", "audit"}} {
        refusal := eventsRefusalOf(t, map[string]eventcontract.EventConfig{kernelcontract.EventKernelRequest: {RunAfterFailureTags: tags}})

        if false == strings.HasPrefix(refusal.Message(), `ApplicationConfig.Events["kernel.request"].RunAfterFailureTags `) {
            t.Fatalf("%v: expected the field named, got %q", tags, refusal.Message())
        }
    }
}

func TestNewApplication_DeclaresTheFrameworkEventsSoALaterDeclarationIsRefused(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})
    dispatcher := applicationInstance.kernel.EventDispatcher()

    declared := make([]string, 0)
    for _, registeredEvent := range dispatcher.RegisteredEvents() {
        if true == registeredEvent.Declared {
            declared = append(declared, registeredEvent.EventName)
        }
    }

    frameworkDeclarations, _ := resolveFrameworkEventConfigs(nil)
    for _, declaration := range frameworkDeclarations {
        if false == slices.Contains(declared, declaration.eventName) {
            t.Fatalf("expected %s declared before the boot, got %v", declaration.eventName, declared)
        }
    }

    testhelper.AssertPanicsWithError(t, func() {
        dispatcher.DeclareEvent(kernelcontract.EventKernelTerminate, eventcontract.EventConfig{})
    }, "already declared")

    dispatcher.DeclareEvent("app.event", eventcontract.EventConfig{RunAfterFailureTags: []string{"audit"}})
}

/* a new framework event constant without a row in frameworkEvents fails here */
func TestFrameworkEvents_ListEveryFrameworkEventConstant(t *testing.T) {
    declarations, _ := resolveFrameworkEventConfigs(nil)
    declaredNames := make([]string, 0, len(declarations))
    for _, declaration := range declarations {
        declaredNames = append(declaredNames, declaration.eventName)
    }

    found := 0
    for _, sourcePath := range []string{"../kernel/contract/event.go", "../security/contract/const.go", "../http/profiling_kernel.go"} {
        parsed, parseErr := parser.ParseFile(token.NewFileSet(), sourcePath, nil, 0)
        if nil != parseErr {
            t.Fatalf("parse %s: %v", sourcePath, parseErr)
        }

        ast.Inspect(parsed, func(node ast.Node) bool {
            valueSpec, isValue := node.(*ast.ValueSpec)
            if false == isValue {
                return true
            }

            for index, name := range valueSpec.Names {
                if false == strings.HasPrefix(name.Name, "Event") || index >= len(valueSpec.Values) {
                    continue
                }

                literal, isLiteral := valueSpec.Values[index].(*ast.BasicLit)
                if false == isLiteral {
                    continue
                }

                eventName, _ := strconv.Unquote(literal.Value)
                found = found + 1
                if false == slices.Contains(declaredNames, eventName) {
                    t.Fatalf("expected %s (%s) among the framework events", name.Name, eventName)
                }
            }

            return true
        })
    }

    if 12 != found {
        t.Fatalf("expected twelve framework event constants, found %d", found)
    }
}
