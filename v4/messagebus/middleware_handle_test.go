package messagebus

import (
    "go/ast"
    "go/parser"
    "go/token"
    "reflect"
    "testing"

    "github.com/precision-soft/melody/v4/internal/testhelper"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

func TestHandle_NoHandlerIsRefusedByDefault(t *testing.T) {
    locator := NewHandlerLocator()

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleMiddlewareConfig{})})

    if _, dispatchErr := bus.Dispatch(newTestRuntime(), taskCreated{TaskId: 1}, messagebuscontract.DispatchOptions{}); nil == dispatchErr {
        t.Fatalf("expected the default middleware to refuse an unhandled message: on the consume path a pass-through is Acked and the message is drained")
    }
}

func TestHandle_AllowMissingHandlerPassesUnhandledMessageThrough(t *testing.T) {
    locator := NewHandlerLocator()

    bus := NewManager(
        "default",
        []messagebuscontract.Middleware{
            NewHandleMessageMiddleware(locator, HandleMiddlewareConfig{AllowMissingHandler: true}),
        },
    )

    if _, dispatchErr := bus.Dispatch(newTestRuntime(), taskCreated{TaskId: 1}, messagebuscontract.DispatchOptions{}); nil != dispatchErr {
        t.Fatalf("expected the opted-in middleware to pass an unhandled message through, got: %v", dispatchErr)
    }
}

func TestHandle_ZeroOptionsRefuse(t *testing.T) {
    locator := NewHandlerLocator()

    bus := NewManager(
        "default",
        []messagebuscontract.Middleware{
            NewHandleMessageMiddleware(locator, HandleMiddlewareConfig{}),
        },
    )

    if _, dispatchErr := bus.Dispatch(newTestRuntime(), taskCreated{TaskId: 1}, messagebuscontract.DispatchOptions{}); nil == dispatchErr {
        t.Fatalf("expected the zero options to refuse an unhandled message")
    }
}

func TestNewHandleMessageMiddleware_RefusesANilLocator(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewHandleMessageMiddleware(nil, HandleMiddlewareConfig{})
    }, "message handler locator is nil")
}

func TestNewHandleMessageMiddleware_TakesAHandleMiddlewareConfig(t *testing.T) {
    constructorType := reflect.TypeOf(NewHandleMessageMiddleware)
    if 2 != constructorType.NumIn() || reflect.TypeOf(HandleMiddlewareConfig{}) != constructorType.In(1) {
        t.Fatalf("expected NewHandleMessageMiddleware(locator, HandleMiddlewareConfig), got %v", constructorType)
    }

    packages, parseErr := parser.ParseDir(token.NewFileSet(), ".", nil, 0)
    if nil != parseErr {
        t.Fatalf("unexpected parse error: %v", parseErr)
    }

    for _, parsedPackage := range packages {
        for fileName, parsedFile := range parsedPackage.Files {
            ast.Inspect(parsedFile, func(node ast.Node) bool {
                typeSpec, isTypeSpec := node.(*ast.TypeSpec)
                if true == isTypeSpec && "HandleOptions" == typeSpec.Name.Name {
                    t.Fatalf("expected no HandleOptions type, found one in %s", fileName)
                }

                return true
            })
        }
    }
}
