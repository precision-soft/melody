package runtime

import (
    "go/ast"
    "go/parser"
    "go/token"
    "go/types"
    "io/fs"
    "os"
    "path/filepath"
    "regexp"
    "strings"
    "testing"
)

/* fourthMajorSourceList lists the Go files of the fourth major: the framework, its example and every v4 integration, the repository read from this package's directory */
func fourthMajorSourceList(t *testing.T) []string {
    t.Helper()

    sourceList := []string{}

    collect := func(root string, insideMajor func(path string) bool) {
        walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
            if nil != err {
                return err
            }

            if true == entry.IsDir() && ("vendor" == entry.Name() || "node_modules" == entry.Name() || ".git" == entry.Name()) {
                return filepath.SkipDir
            }

            if false == entry.IsDir() && true == strings.HasSuffix(path, ".go") && true == insideMajor(path) {
                sourceList = append(sourceList, path)
            }

            return nil
        })
        if nil != walkErr {
            t.Fatalf("walk %s: %v", root, walkErr)
        }
    }

    collect(filepath.Join("..", ".."), func(path string) bool { return true })

    integrationRoot := filepath.Join("..", "..", "..", "integrations")
    if _, statErr := os.Stat(integrationRoot); nil == statErr {
        collect(integrationRoot, func(path string) bool {
            return true == strings.Contains(filepath.ToSlash(path), "/v4/")
        })
    }

    if 100 > len(sourceList) {
        t.Fatalf("expected the fourth major's sources, found %d files", len(sourceList))
    }

    return sourceList
}

func TestSourceWalk_NoFileImportsTheRemovedRuntimePackages(t *testing.T) {
    removedImport := regexp.MustCompile(`"github\.com/precision-soft/melody/v4/runtime(/contract)?"`)

    for _, source := range fourthMajorSourceList(t) {
        content, readErr := os.ReadFile(source)
        if nil != readErr {
            t.Fatalf("read %s: %v", source, readErr)
        }

        if true == removedImport.Match(content) {
            t.Fatalf("%s imports a removed runtime package; the runtime lives in v4/container/runtime and v4/container/contract", source)
        }
    }
}

/* a runtime rebuilt from another's scope takes a kind the caller names, where WithContext keeps the one it had */
func TestSourceWalk_NoProductionFileRebuildsARuntimeFromAnothersScope(t *testing.T) {
    rebuilt := regexp.MustCompile(`New(Request|Run)\(\s*[^,()]+(\([^()]*\))?,\s*[A-Za-z_][A-Za-z0-9_]*\.Scope\(\)\s*,?\s*\)`)

    for _, source := range fourthMajorSourceList(t) {
        if true == strings.HasSuffix(source, "_test.go") {
            continue
        }

        content, readErr := os.ReadFile(source)
        if nil != readErr {
            t.Fatalf("read %s: %v", source, readErr)
        }

        if match := rebuilt.Find(content); nil != match {
            t.Fatalf("%s rebuilds a runtime from another runtime's scope (%s); derive it with WithContext", source, match)
        }
    }
}

func TestSourceWalk_NoFileNamesTheRemovedResolutionContextDoors(t *testing.T) {
    /* spelled in pieces so this file does not match itself */
    removed := regexp.MustCompile(`(^|[^A-Za-z0-9_])(Context` + `Of\b|Resolution` + `Context\b)`)

    for _, source := range fourthMajorSourceList(t) {
        content, readErr := os.ReadFile(source)
        if nil != readErr {
            t.Fatalf("read %s: %v", source, readErr)
        }

        if match := removed.Find(content); nil != match {
            t.Fatalf("%s names a removed door (%s); read resolver.Runtime().Context()", source, match)
        }
    }
}

/* an accessor a provider reads stays resolver-only, and one whose bound view is still to come waits for it; every other named accessor has a runtime form beside it */
var accessorWithoutRuntimeForm = map[string]string{
    "v4/container:MustFromResolver":                                 "generic door, its runtime form is runtime.MustFromRuntime",
    "v4/container:MustFromResolverByType":                           "generic door, by type, resolved through runtime.MustFromRuntime by name",
    "v4/cache:CacheBackendMustFromResolver":                         "provider",
    "v4/cache:CacheSerializerMustFromResolver":                      "provider",
    "v4/messagebus:ConsumeBusFromResolver":                          "provider",
    "v4/messagebus:TransportsMustFromResolver":                      "provider",
    "v4/messagebus:RetryPolicyFromResolver":                         "provider",
    "v4/messagebus:HandlerLocatorMustFromResolver":                  "provider",
    "v4/session:SessionStorageMustFromResolver":                     "provider",
    "v4/openapi:RegistryMustFromResolver":                           "provider",
    "v4/openapi:InfoFromResolver":                                   "provider",
    "integrations/amqp/v4:TransportMustFromResolver":                "provider",
    "integrations/outbox/v4:RelayMustFromResolver":                  "provider",
    "integrations/outbox/v4:NewRelayCommandFromResolver":            "constructor",
    "v4/cache:CacheMustFromResolver":                                "view",
    "v4/storage:StorageMustFromResolver":                            "view",
    "integrations/rueidis/v4:TokenStoreMustFromResolver":            "view",
    "integrations/rueidis/v4:EpochRevocableTokenStoreMustFromResolver": "view",
}

func TestSourceWalk_EveryNamedResolverAccessorHasARuntimeFormOrADecision(t *testing.T) {
    /* a generic accessor and a suffixed one count too; the twin is looked for in the same file */
    accessor := regexp.MustCompile(`(?m)^func ([A-Z][A-Za-z0-9]*)FromResolver([A-Za-z0-9]*)[\[(]`)
    seen := map[string]bool{}
    _, integrationErr := os.Stat(filepath.Join("..", "..", "..", "integrations"))

    for _, source := range fourthMajorSourceList(t) {
        slashed := filepath.ToSlash(source)
        if true == strings.HasSuffix(source, "_test.go") || true == strings.Contains(slashed, "/.example/") {
            continue
        }

        content, readErr := os.ReadFile(source)
        if nil != readErr {
            t.Fatalf("read %s: %v", source, readErr)
        }

        directory := filepath.ToSlash(filepath.Dir(source))
        if true == strings.HasPrefix(directory, "../../../integrations/") {
            directory = strings.TrimPrefix(directory, "../../../")
        } else {
            directory = "v4/" + strings.TrimPrefix(directory, "../../")
        }

        for _, match := range accessor.FindAllSubmatch(content, -1) {
            stem := string(match[1])
            suffix := string(match[2])
            key := directory + ":" + stem + "FromResolver" + suffix
            seen[key] = true

            hasTwin := regexp.MustCompile(`(?m)^func ` + stem + `FromRuntime` + suffix + `[\[(]`).Match(content)

            if _, decided := accessorWithoutRuntimeForm[key]; true == decided {
                if true == hasTwin {
                    t.Errorf("%s has a runtime form now; drop it from the list", key)
                }

                continue
            }

            if false == hasTwin {
                t.Errorf("%s declares %sFromResolver%s without %sFromRuntime%s beside it; add the runtime form, or list it as provider-only or as waiting for its view", source, stem, suffix, stem, suffix)
            }
        }
    }

    for key := range accessorWithoutRuntimeForm {
        /* a tree without the integrations cannot judge their decisions */
        if nil != integrationErr && true == strings.HasPrefix(key, "integrations/") {
            continue
        }

        if false == seen[key] {
            t.Errorf("the decision for %s names no accessor any more; drop it from the list", key)
        }
    }
}

/* the calls a runtime-holding framework file still makes with a runtime's context and its scope apart, each counted; a provider-only accessor's decision is the one in accessorWithoutRuntimeForm */
var runtimeHeldResolverCallAllowed = map[string]runtimeHeldResolverAllowance{
    "v4/openapi/generate_command.go:InfoFromResolver":             {count: 1, decision: "provider"},
    "v4/openapi/generate_command.go:RegistryMustFromResolver":     {count: 1, decision: "provider"},
    "v4/messagebus/consume_command.go:ConsumeBusFromResolver":     {count: 1, decision: "provider"},
    "v4/messagebus/consume_command.go:TransportsMustFromResolver": {count: 1, decision: "provider"},
    "v4/messagebus/consume_command.go:RetryPolicyFromResolver":    {count: 1, decision: "provider"},
    "v4/http/router_utility.go:GetByType":                         {count: 1, decision: "by type, which has no runtime form"},
}

type runtimeHeldResolverAllowance struct {
    count    int
    decision string
}

type runtimeHeldResolverCall struct {
    callee string
    line   int
}

/* runtimeHeldResolverCalls answers the calls of one source that take a runtime's context and the same runtime's scope apart, as their two leading arguments or as a method of the scope handed the context; a context or a scope held in a local of the same function counts, and so does the scope a runtime was built over */
func runtimeHeldResolverCalls(source string, content []byte) ([]runtimeHeldResolverCall, error) {
    fileSet := token.NewFileSet()
    file, parseErr := parser.ParseFile(fileSet, source, content, parser.SkipObjectResolution)
    if nil != parseErr {
        return nil, parseErr
    }

    callList := []runtimeHeldResolverCall{}

    for _, declaration := range file.Decls {
        function, isFunction := declaration.(*ast.FuncDecl)
        if false == isFunction || nil == function.Body {
            continue
        }

        contextOf := map[string]string{}
        scopeOf := map[string]string{}

        bind := func(left ast.Expr, right ast.Expr) {
            name := types.ExprString(ast.Unparen(left))
            if receiver := runtimeOfContext(right, contextOf); "" != receiver {
                contextOf[name] = receiver
            }

            if receiver := runtimeOfScope(right, scopeOf); "" != receiver {
                scopeOf[name] = receiver
            }

            if built, isCall := ast.Unparen(right).(*ast.CallExpr); true == isCall && 2 == len(built.Args) {
                if constructor := calleeName(built.Fun); "NewRun" == constructor || "NewRequest" == constructor {
                    scopeOf[types.ExprString(ast.Unparen(built.Args[1]))] = name
                }
            }
        }

        ast.Inspect(function.Body, func(node ast.Node) bool {
            switch statement := node.(type) {
            case *ast.AssignStmt:
                if len(statement.Lhs) == len(statement.Rhs) {
                    for index := range statement.Lhs {
                        bind(statement.Lhs[index], statement.Rhs[index])
                    }
                } else if 1 == len(statement.Rhs) {
                    bind(statement.Lhs[0], statement.Rhs[0])
                }
            case *ast.ValueSpec:
                if len(statement.Names) == len(statement.Values) {
                    for index := range statement.Names {
                        bind(statement.Names[index], statement.Values[index])
                    }
                } else if 1 == len(statement.Values) && 0 < len(statement.Names) {
                    bind(statement.Names[0], statement.Values[0])
                }
            }

            return true
        })

        ast.Inspect(function.Body, func(node ast.Node) bool {
            call, isCall := node.(*ast.CallExpr)
            if false == isCall || 0 == len(call.Args) {
                return true
            }

            callee := calleeName(call.Fun)
            if "NewRun" == callee || "NewRequest" == callee || "NewChildRun" == callee {
                return true
            }

            contextReceiver := runtimeOfContext(call.Args[0], contextOf)
            if "" == contextReceiver {
                return true
            }

            if 2 <= len(call.Args) && contextReceiver == runtimeOfScope(call.Args[1], scopeOf) {
                callList = append(callList, runtimeHeldResolverCall{callee: callee, line: fileSet.Position(call.Pos()).Line})

                return true
            }

            if method, isMethod := ast.Unparen(call.Fun).(*ast.SelectorExpr); true == isMethod && contextReceiver == runtimeOfScope(method.X, scopeOf) {
                callList = append(callList, runtimeHeldResolverCall{callee: callee, line: fileSet.Position(call.Pos()).Line})
            }

            return true
        })
    }

    return callList, nil
}

/* runtimeOfContext answers the runtime whose Context() the expression is, holds, or derives a context from as its first argument */
func runtimeOfContext(expression ast.Expr, contextOf map[string]string) string {
    switch typed := ast.Unparen(expression).(type) {
    case *ast.CallExpr:
        if selector, isSelector := ast.Unparen(typed.Fun).(*ast.SelectorExpr); true == isSelector && "Context" == selector.Sel.Name && 0 == len(typed.Args) {
            return types.ExprString(ast.Unparen(selector.X))
        }

        if 0 < len(typed.Args) {
            return runtimeOfContext(typed.Args[0], contextOf)
        }
    case *ast.Ident, *ast.SelectorExpr:
        return contextOf[types.ExprString(typed)]
    }

    return ""
}

/* runtimeOfScope answers the runtime whose Scope() the expression is or holds, through a type assertion */
func runtimeOfScope(expression ast.Expr, scopeOf map[string]string) string {
    switch typed := ast.Unparen(expression).(type) {
    case *ast.CallExpr:
        if selector, isSelector := ast.Unparen(typed.Fun).(*ast.SelectorExpr); true == isSelector && "Scope" == selector.Sel.Name && 0 == len(typed.Args) {
            return types.ExprString(ast.Unparen(selector.X))
        }
    case *ast.TypeAssertExpr:
        return runtimeOfScope(typed.X, scopeOf)
    case *ast.Ident, *ast.SelectorExpr:
        return scopeOf[types.ExprString(typed)]
    }

    return ""
}

func calleeName(function ast.Expr) string {
    switch typed := ast.Unparen(function).(type) {
    case *ast.IndexExpr:
        return calleeName(typed.X)
    case *ast.IndexListExpr:
        return calleeName(typed.X)
    case *ast.SelectorExpr:
        return typed.Sel.Name
    case *ast.Ident:
        return typed.Name
    }

    return ""
}

/* code holding a runtime resolves through it; a resolver door handed the runtime's context and scope bypasses the runtime's refusals */
func TestSourceWalk_NoFrameworkFileHandsARuntimesContextAndScopeToAResolverDoor(t *testing.T) {
    seen := map[string]int{}

    for key, allowance := range runtimeHeldResolverCallAllowed {
        if "provider" != allowance.decision {
            continue
        }

        directory := key[:strings.LastIndex(key[:strings.Index(key, ":")], "/")]
        decisionKey := directory + key[strings.Index(key, ":"):]
        if "provider" != accessorWithoutRuntimeForm[decisionKey] {
            t.Errorf("the allowance for %s names an accessor %s does not list as provider-only", key, decisionKey)
        }
    }

    for _, source := range fourthMajorSourceList(t) {
        slashed := filepath.ToSlash(source)
        if true == strings.HasSuffix(source, "_test.go") || true == strings.Contains(slashed, "/.example/") {
            continue
        }

        content, readErr := os.ReadFile(source)
        if nil != readErr {
            t.Fatalf("read %s: %v", source, readErr)
        }

        file := "v4/" + strings.TrimPrefix(slashed, "../../")
        if true == strings.HasPrefix(slashed, "../../../integrations/") {
            file = strings.TrimPrefix(slashed, "../../../")
        }

        callList, parseErr := runtimeHeldResolverCalls(source, content)
        if nil != parseErr {
            t.Fatalf("parse %s: %v", source, parseErr)
        }

        for _, call := range callList {
            key := file + ":" + call.callee
            if _, allowed := runtimeHeldResolverCallAllowed[key]; true == allowed {
                seen[key]++

                continue
            }

            t.Errorf("%s:%d hands %s a runtime's context and scope apart; resolve through the runtime (XMustFromRuntime(runtimeInstance), runtime.FromRuntime[T])", file, call.line, call.callee)
        }
    }

    for key, allowance := range runtimeHeldResolverCallAllowed {
        if allowance.count != seen[key] {
            t.Errorf("the allowance for %s counts %d calls, the tree holds %d", key, allowance.count, seen[key])
        }
    }
}

func TestRuntimeHeldResolverCalls_FindsEachSpelling(t *testing.T) {
    content := []byte(`package probe

func probe(runtimeInstance Runtime, request Request, list []Runtime) {
    _ = config.ConfigMustFromResolver(runtimeInstance.Context(), runtimeInstance.Scope())
    _, _ = container.FromResolver[Configuration](
        request.RuntimeInstance().Context(),
        request.RuntimeInstance().Scope(),
        name,
    )
    _ = container.FromResolver[[]Foo](runtimeInstance.Context(), runtimeInstance.Scope(), name)
    _ = container.MustFromResolver[map[string]Foo](runtimeInstance.Context(), runtimeInstance.Scope(), name)
    _ = container.MustFromResolver[*pkg.Type[int]]((runtimeInstance).Context(), runtimeInstance.Scope(), name)
    _ = Door(list[0].Context(), list[0].Scope())
    _ = Door(a.B().C().Context(), a.B().C().Scope())
    _ = Door(context.WithoutCancel(runtimeInstance.Context()), runtimeInstance.Scope())
    scope := runtimeInstance.Scope()
    _ = logging.LoggerMustFromResolver(runtimeInstance.Context(), scope)
    ctx := runtimeInstance.Context()
    _ = Door(ctx, runtimeInstance.Scope())
    _ = Door(ctx, scope)
    var typedScope Scope = runtimeInstance.Scope()
    _ = Door(ctx, typedScope)
    asserted, ok := runtimeInstance.Scope().(Scoped)
    _ = Door(ctx, asserted)
    session.scope = runtimeInstance.Scope()
    _ = Door(ctx, session.scope)
    built := runtime.NewRun(ctx, runScope)
    _ = event.EventDispatcherMustFromResolver(built.Context(), runScope)
    _, _ = runtimeInstance.Scope().Get(runtimeInstance.Context(), name)
    _, _ = scope.GetByType(ctx, paramType)
    /* config.ConfigMustFromResolver(runtimeInstance.Context(), runtimeInstance.Scope()) */
    _ = "Door(runtimeInstance.Context(), runtimeInstance.Scope())"
    _ = runtime.NewRun(runtimeInstance.Context(), runtimeInstance.Scope())
    _ = config.ConfigMustFromResolver(ctx, resolver)
    _ = config.ConfigMustFromResolver(first.Context(), second.Scope())
    _ = ok
}

func other(runtimeInstance Runtime, scope Resolver) {
    _ = Door(runtimeInstance.Context(), scope)
}
`)

    callList, parseErr := runtimeHeldResolverCalls("probe.go", content)
    if nil != parseErr {
        t.Fatalf("parse: %v", parseErr)
    }

    expected := []runtimeHeldResolverCall{
        {callee: "ConfigMustFromResolver", line: 4},
        {callee: "FromResolver", line: 5},
        {callee: "FromResolver", line: 10},
        {callee: "MustFromResolver", line: 11},
        {callee: "MustFromResolver", line: 12},
        {callee: "Door", line: 13},
        {callee: "Door", line: 14},
        {callee: "Door", line: 15},
        {callee: "LoggerMustFromResolver", line: 17},
        {callee: "Door", line: 19},
        {callee: "Door", line: 20},
        {callee: "Door", line: 22},
        {callee: "Door", line: 24},
        {callee: "Door", line: 26},
        {callee: "EventDispatcherMustFromResolver", line: 28},
        {callee: "Get", line: 29},
        {callee: "GetByType", line: 30},
    }

    if len(expected) != len(callList) {
        t.Fatalf("expected %v, got %v", expected, callList)
    }

    for index, call := range expected {
        if call != callList[index] {
            t.Fatalf("expected %v at %d, got %v", call, index, callList[index])
        }
    }
}
