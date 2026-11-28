package application

import (
    "context"
    "errors"
    "go/ast"
    "go/parser"
    "go/token"
    "slices"
    "strconv"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

func declaredEventsOf(t *testing.T, events map[string]eventcontract.EventConfig) map[string][]string {
    t.Helper()

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{})
    if declareErr := declareFrameworkEvents(dispatcher, events); nil != declareErr {
        t.Fatalf("unexpected refusal: %v", declareErr)
    }

    declared := make(map[string][]string)
    for _, registeredEvent := range dispatcher.RegisteredEvents() {
        if true == registeredEvent.Declared {
            declared[registeredEvent.EventName] = registeredEvent.Config.TagsToRunAfterFailure
        }
    }

    return declared
}

func TestDeclareFrameworkEvents_DeclaresTheTwelveFrameworkEvents(t *testing.T) {
    declared := declaredEventsOf(t, nil)
    if 12 != len(declared) {
        t.Fatalf("expected twelve declarations, got %d: %v", len(declared), declared)
    }

    for eventName, tags := range declared {
        expected := ""
        switch eventName {
        case kernelcontract.EventKernelResponse:
            expected = kernelcontract.ListenerTagKernelCors + "," + kernelcontract.ListenerTagKernelSecurityHeaders
        case kernelcontract.EventKernelTerminate:
            expected = kernelcontract.ListenerTagKernelAccessLog
        }

        if expected != strings.Join(tags, ",") {
            t.Fatalf("%s: expected tags %q, got %v", eventName, expected, tags)
        }
    }
}

func TestDeclareFrameworkEvents_DeclaresTheApplicationsTagsAfterTheFrameworks(t *testing.T) {
    applicationTags := []string{"audit", "cache"}

    declared := declaredEventsOf(t, map[string]eventcontract.EventConfig{
        kernelcontract.EventKernelResponse: {TagsToRunAfterFailure: applicationTags},
        kernelcontract.EventKernelRequest:  {TagsToRunAfterFailure: []string{"audit"}},
    })

    if "melody.cors,melody.security_headers,audit,cache" != strings.Join(declared[kernelcontract.EventKernelResponse], ",") {
        t.Fatalf("expected the framework's tags first, got %v", declared[kernelcontract.EventKernelResponse])
    }

    if "audit" != strings.Join(declared[kernelcontract.EventKernelRequest], ",") {
        t.Fatalf("expected an event without framework tags to carry the application's alone, got %v", declared[kernelcontract.EventKernelRequest])
    }
}

func TestDeclareFrameworkEvents_AllowsAFrameworkTagOnAnotherFrameworkEvent(t *testing.T) {
    declared := declaredEventsOf(t, map[string]eventcontract.EventConfig{kernelcontract.EventKernelRequest: {TagsToRunAfterFailure: []string{kernelcontract.ListenerTagKernelCors}}})

    if kernelcontract.ListenerTagKernelCors != strings.Join(declared[kernelcontract.EventKernelRequest], ",") {
        t.Fatalf("expected the framework tag declared on kernel.request, got %v", declared[kernelcontract.EventKernelRequest])
    }
}

func eventsRefusalOf(t *testing.T, events map[string]eventcontract.EventConfig) (*exception.Error, eventcontract.EventDispatcher) {
    t.Helper()

    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{})
    declareErr := declareFrameworkEvents(dispatcher, events)

    refusal, isRefusal := declareErr.(*exception.Error)
    if false == isRefusal {
        t.Fatalf("expected a refusal, got %v", declareErr)
    }

    return refusal, dispatcher
}

func TestDeclareFrameworkEvents_RefusesAnEventTheFrameworkDoesNotOwn(t *testing.T) {
    refusal, dispatcher := eventsRefusalOf(t, map[string]eventcontract.EventConfig{"user.deleted": {}, "app.created": {}, kernelcontract.EventKernelRequest: {}})

    if "ApplicationConfig.Events names an event the framework does not own; an application event is declared through EventDispatcher.DeclareEvent" != refusal.Message() || "app.created" != refusal.Context()["eventName"] {
        t.Fatalf("expected the sorted first unknown event named, got %q %v", refusal.Message(), refusal.Context())
    }

    for _, registeredEvent := range dispatcher.RegisteredEvents() {
        if true == registeredEvent.Declared {
            t.Fatalf("expected nothing declared ahead of the ownership refusal, got %s", registeredEvent.EventName)
        }
    }
}

func TestDeclareFrameworkEvents_AnswersTheDispatchersRefusalNamingTheApplicationsField(t *testing.T) {
    for _, testCase := range []struct {
        name      string
        eventName string
        tags      []string
        reason    string
    }{
        {"empty", kernelcontract.EventKernelRequest, []string{""}, "may not carry an empty tag"},
        {"whitespace", kernelcontract.EventKernelRequest, []string{"a b"}, "carries a tag with whitespace"},
        {"twice", kernelcontract.EventKernelRequest, []string{"audit", "audit"}, "carries a tag twice"},
        {"reserved", kernelcontract.EventKernelRequest, []string{"melody.anything"}, "carries a tag in the melody. namespace the framework does not define"},
        {"a framework tag repeated on its own event", kernelcontract.EventKernelResponse, []string{kernelcontract.ListenerTagKernelCors}, "carries a tag twice"},
    } {
        refusal, _ := eventsRefusalOf(t, map[string]eventcontract.EventConfig{testCase.eventName: {TagsToRunAfterFailure: testCase.tags}})

        if "ApplicationConfig.Events is refused" != refusal.Message() || `ApplicationConfig.Events["`+testCase.eventName+`"].TagsToRunAfterFailure` != refusal.Context()["field"] || testCase.eventName != refusal.Context()["eventName"] {
            t.Fatalf("%s: expected the application's field named, got %q %v", testCase.name, refusal.Message(), refusal.Context())
        }

        var cause *exception.Error
        if false == errors.As(refusal.Unwrap(), &cause) || "EventConfig.TagsToRunAfterFailure "+testCase.reason != cause.Message() {
            t.Fatalf("%s: expected the dispatcher's refusal as the cause, got %v", testCase.name, refusal.Unwrap())
        }
    }
}

type panickingDeclarationDispatcher struct {
    eventcontract.EventDispatcher
}

func (instance *panickingDeclarationDispatcher) DeclareEvent(string, eventcontract.EventConfig) {
    panic("not a refusal")
}

func TestDeclareFrameworkEvents_APanicThatIsNoRefusalPasses(t *testing.T) {
    defer func() {
        if "not a refusal" != recover() {
            t.Fatalf("expected the panic to pass")
        }
    }()

    _ = declareFrameworkEvents(&panickingDeclarationDispatcher{EventDispatcher: event.NewEventDispatcher(event.EventDispatcherConfig{})}, nil)
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

    for _, declaration := range frameworkEvents() {
        if false == slices.Contains(declared, declaration.eventName) {
            t.Fatalf("expected %s declared before the boot, got %v", declaration.eventName, declared)
        }
    }

    testhelper.AssertPanicsWithError(t, func() {
        dispatcher.DeclareEvent(kernelcontract.EventKernelTerminate, eventcontract.EventConfig{})
    }, "already declared")

    dispatcher.DeclareEvent("app.event", eventcontract.EventConfig{TagsToRunAfterFailure: []string{"audit"}})
}

func TestFrameworkEvents_ListEveryFrameworkEventConstant(t *testing.T) {
    declarations := frameworkEvents()
    declaredNames := make([]string, 0, len(declarations))
    for _, declaration := range declarations {
        declaredNames = append(declaredNames, declaration.eventName)
    }

    found := 0
    for _, sourcePath := range []string{"../kernel/contract/event.go", "../security/contract/const.go", "../http/contract/event.go"} {
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
