package runtime

import (
    "go/ast"
    "go/parser"
    "go/token"
    "go/types"
    "os"
    "path/filepath"
    "regexp"
    "strconv"
    "strings"
    "testing"
)

/* the exported constructors outside the argument rule, each with the reason it stays; an entry the walk does not meet fails */
var constructorArgumentRuleException = map[string]string{
    "v4/kernel:NewKernel":                    "framework-only behind the kernelseam token: the application builds it from its own collaborators",
    "v4/validation:NewValidationError":       "the three values of every validation error and its params map, built at each constraint",
    "integrations/rueidis/v4:NewRateLimiter": "required collaborators and values, connection, name, limit and window, then the config struct",
}

/* the config fields refused as required, each with the reason they stay; an entry the walk does not meet fails */
var configRequiredFieldException = map[string]string{
    "v4/lock:InMemoryLockerConfig.Clock":                             "optional, the system clock when left out; only a typed nil is refused",
    "v4/translation:ManagerConfig.FallbackLocales":                   "required only beside catalogs, and an empty entry of the list is refused by its index",
    "integrations/bunorm/v4/audit:RecorderConfig.Logger":             "the required logger convention is decided once for every module",
    "integrations/bunorm/v4/audit:AsyncStorageConfig.Logger":         "the required logger convention is decided once for every module",
    "integrations/bunorm/v4/encrypt:ModuleConfig.Cipher":             "a module's one argument is its declaration",
    "integrations/bunorm/v4/encrypt:CommandContextConfig.Name":       "an entry of the module's context list cannot be positional",
    "integrations/bunorm/v4/encrypt:CommandContextConfig.Cipher":     "an entry of the module's context list cannot be positional",
    "integrations/websocket/v4:ModuleConfig.Streams":                 "an entry of the module's stream list cannot be positional; the refusal names the index",
    "v4/security:CompiledFirewallConfig.Name":                        "a firewall's name is its declaration, which the security configuration's list of firewalls carries",
    "v4/httpclient:HttpClientConfig.RoundTripperDecorators":          "optional, the transport alone when left out; only a nil entry of the list is refused, by its index",
    "v4/http/static:Config.FileSystem":                               "required only in the embedded mode; the filesystem mode builds it from the public directory",
}

/* the framework-only names, keyed by package and receiver, none declared exported */
var frameworkOnlySymbol = []string{
    "v4/config:NewParameter",
    "v4/event:NewRequiredListenerSkippedError",
    "v4/http:NewErrorResponsePayload",
    "v4/http:NewHttpRequestProfile",
    "v4/http:NewRouteDefinition",
    "v4/http:NewUrlGenerationRouteDefinition",
    "v4/http:RouteRegistry.SetBootCollisionRecorder",
    "v4/http/middleware/pipeline:MiddlewareBuildReport.SetCycleDetected",
    "v4/http/middleware/pipeline:MiddlewareBuildReport.SetInactive",
    "v4/http/middleware/pipeline:MiddlewareBuildReport.SetMissingReference",
    "v4/http/middleware/pipeline:MiddlewareBuildReport.SetSelectedNames",
    "v4/http/middleware/pipeline:NewInactiveMiddleware",
    "v4/http/middleware/pipeline:NewMiddlewareBuildReport",
    "v4/security:NewMatchedAccessControlRule",
    "v4/security:SecurityContext.SetMatchedRule",
}

/* the framework-only names that left the surface under another name, each with the name that took its place */
var frameworkOnlySymbolSuccessor = map[string]string{
    "v4/http:RouteRegistry.SetBootCollisionRecorder": "RouteRegistry.InstallBootCollisionRecorder",
}

/* unexportedTwin answers the name an entry is declared under once unexported: the function's, or the method's, first letter lowered */
func unexportedTwin(name string) string {
    separatorIndex := max(strings.LastIndex(name, "."), strings.LastIndex(name, ":"))
    prefix, function := name[:separatorIndex+1], name[separatorIndex+1:]

    return prefix + strings.ToLower(function[:1]) + function[1:]
}

type productionSource struct {
    key  string
    file *ast.File
    set  *token.FileSet
}

/* fourthMajorProductionSourceList parses the production files of the framework and its integrations, tests, internal packages and the example left out, each keyed by its package directory */
func fourthMajorProductionSourceList(t *testing.T) []productionSource {
    t.Helper()

    sources := []productionSource{}

    for _, source := range fourthMajorSourceList(t) {
        slashed := filepath.ToSlash(source)
        if true == strings.HasSuffix(slashed, "_test.go") || true == strings.Contains(slashed, "/internal/") || true == strings.Contains(slashed, "/.example/") || true == strings.Contains(slashed, "/.dev/") {
            continue
        }

        fileSet := token.NewFileSet()
        file, parseErr := parser.ParseFile(fileSet, source, nil, 0)
        if nil != parseErr {
            t.Fatalf("parse %s: %v", source, parseErr)
        }

        sources = append(sources, productionSource{key: packageKey(source), file: file, set: fileSet})
    }

    return sources
}

func packageKey(source string) string {
    directory := filepath.ToSlash(filepath.Dir(source))
    if true == strings.HasPrefix(directory, "../../../integrations/") {
        return strings.TrimPrefix(directory, "../../../")
    }

    if "../.." == directory {
        return "v4"
    }

    return "v4/" + strings.TrimPrefix(directory, "../../")
}

func integrationsPresent() bool {
    _, statErr := os.Stat(filepath.Join("..", "..", "..", "integrations"))

    return nil == statErr
}

func TestExportedConstructors_FollowTheArgumentRule(t *testing.T) {
    seen := map[string]bool{}
    constructors := 0

    for _, source := range fourthMajorProductionSourceList(t) {
        for _, declaration := range source.file.Decls {
            function, isFunction := declaration.(*ast.FuncDecl)
            if false == isFunction || nil != function.Recv || false == strings.HasPrefix(function.Name.Name, "New") || false == function.Name.IsExported() {
                continue
            }

            constructors++
            key := source.key + ":" + function.Name.Name
            violation := constructorArgumentViolation(function)
            if "" == violation {
                continue
            }

            seen[key] = true
            if _, excepted := constructorArgumentRuleException[key]; true == excepted {
                continue
            }

            t.Errorf("%s: %s %s; required values are positional up to three, the rest in a config struct", source.set.Position(function.Pos()), key, violation)
        }
    }

    if 200 > constructors {
        t.Fatalf("expected the fourth major's constructors, walked %d", constructors)
    }

    for key := range constructorArgumentRuleException {
        if false == integrationsPresent() && true == strings.HasPrefix(key, "integrations/") {
            continue
        }

        if false == seen[key] {
            t.Errorf("the exception for %s matches no constructor outside the rule any more; drop it", key)
        }
    }
}

func constructorArgumentViolation(function *ast.FuncDecl) string {
    typeParameters := map[string]bool{}
    if nil != function.Type.TypeParams {
        for _, field := range function.Type.TypeParams.List {
            for _, name := range field.Names {
                typeParameters[name.Name] = true
            }
        }
    }

    parameterTypes := []ast.Expr{}
    for _, field := range function.Type.Params.List {
        count := len(field.Names)
        if 0 == count {
            count = 1
        }

        for index := 0; index < count; index++ {
            parameterTypes = append(parameterTypes, field.Type)
        }
    }

    for _, parameterType := range parameterTypes {
        base := baseTypeName(parameterType)
        if "bool" == base {
            return "takes a bool"
        }

        if "" != base && false == ast.IsExported(base) && false == typeParameters[base] && nil == types.Universe.Lookup(base) {
            return "takes the unexported type " + base
        }
    }

    positional := len(parameterTypes)
    if 0 < positional && true == strings.HasSuffix(baseTypeName(parameterTypes[positional-1]), "Config") {
        positional--
    }

    if 3 < positional {
        return "takes " + strconv.Itoa(positional) + " positional parameters"
    }

    return ""
}

/* baseTypeName answers the name a parameter type is spelled by under its pointers, slices, maps and ellipsis, the selector's name for a qualified one, "" for a func or an interface literal */
func baseTypeName(expression ast.Expr) string {
    switch typed := expression.(type) {
    case *ast.Ident:
        return typed.Name
    case *ast.SelectorExpr:
        return typed.Sel.Name
    case *ast.StarExpr:
        return baseTypeName(typed.X)
    case *ast.ArrayType:
        return baseTypeName(typed.Elt)
    case *ast.Ellipsis:
        return baseTypeName(typed.Elt)
    case *ast.MapType:
        return baseTypeName(typed.Value)
    case *ast.IndexExpr:
        return baseTypeName(typed.X)
    case *ast.IndexListExpr:
        return baseTypeName(typed.X)
    }

    return ""
}

func TestConfigStructs_HoldNoRequiredField(t *testing.T) {
    requiredMessage := regexp.MustCompile(`is nil|is empty|is required|needs|may not be`)
    configField := regexp.MustCompile(`^([A-Z][A-Za-z0-9]*)?Config\.[A-Z][A-Za-z0-9]*`)
    seen := map[string]bool{}
    refusals := 0

    for _, source := range fourthMajorProductionSourceList(t) {
        ast.Inspect(source.file, func(node ast.Node) bool {
            call, isCall := node.(*ast.CallExpr)
            if false == isCall || 0 == len(call.Args) {
                return true
            }

            message, isMessage := stringLiteralValue(call.Args[0])
            if false == isMessage || false == requiredMessage.MatchString(message) {
                return true
            }

            refusals++
            ast.Inspect(call, func(inner ast.Node) bool {
                pair, isPair := inner.(*ast.KeyValueExpr)
                if false == isPair {
                    return true
                }

                if key, isKey := stringLiteralValue(pair.Key); false == isKey || "field" != key {
                    return true
                }

                value, isValue := stringLiteralValue(pair.Value)
                if false == isValue {
                    if formatted, isFormatted := pair.Value.(*ast.CallExpr); true == isFormatted && 0 < len(formatted.Args) {
                        value, isValue = stringLiteralValue(formatted.Args[0])
                    }
                }

                field := configField.FindString(value)
                if false == isValue || "" == field {
                    return true
                }

                exceptionKey := source.key + ":" + field
                seen[exceptionKey] = true
                if _, excepted := configRequiredFieldException[exceptionKey]; true == excepted {
                    return true
                }

                t.Errorf("%s: %s is refused as %q; a required value is a positional argument", source.set.Position(pair.Pos()), exceptionKey, message)

                return true
            })

            return true
        })
    }

    /* a refusal that names no field is met through the guard that raises it: a config parameter's field compared with its zero value */
    for _, source := range fourthMajorProductionSourceList(t) {
        for _, declaration := range source.file.Decls {
            function, isFunction := declaration.(*ast.FuncDecl)
            if false == isFunction || nil == function.Body {
                continue
            }

            configParameters := map[string]string{}
            for _, parameter := range function.Type.Params.List {
                typeName := baseTypeName(parameter.Type)
                if false == strings.HasSuffix(typeName, "Config") {
                    continue
                }

                for _, name := range parameter.Names {
                    configParameters[name.Name] = typeName
                }
            }

            if 0 == len(configParameters) {
                continue
            }

            ast.Inspect(function.Body, func(node ast.Node) bool {
                guard, isGuard := node.(*ast.IfStmt)
                if false == isGuard {
                    return true
                }

                field := guardedConfigField(guard.Cond, configParameters)
                if "" == field {
                    return true
                }

                ast.Inspect(guard.Body, func(inner ast.Node) bool {
                    call, isCall := inner.(*ast.CallExpr)
                    if false == isCall || 0 == len(call.Args) {
                        return true
                    }

                    message, isMessage := stringLiteralValue(call.Args[0])
                    if false == isMessage || false == requiredMessage.MatchString(message) || true == namesAContextKey(call) {
                        return true
                    }

                    refusals++
                    exceptionKey := source.key + ":" + field
                    seen[exceptionKey] = true
                    if _, excepted := configRequiredFieldException[exceptionKey]; true == excepted {
                        return false
                    }

                    t.Errorf("%s: %s is refused as %q, naming no field; a required value is a positional argument", source.set.Position(call.Pos()), exceptionKey, message)

                    return false
                })

                return true
            })
        }
    }

    if 50 > refusals {
        t.Fatalf("expected the fourth major's required-value refusals, walked %d", refusals)
    }

    for key := range configRequiredFieldException {
        if false == integrationsPresent() && true == strings.HasPrefix(key, "integrations/") {
            continue
        }

        if false == seen[key] {
            t.Errorf("the exception for %s matches no refusal any more; drop it", key)
        }
    }
}

/* guardedConfigField answers "<Type>.<Field>" when the condition compares a field of a config parameter with "" or nil, or asks IsNilInterface of it */
func guardedConfigField(condition ast.Expr, configParameters map[string]string) string {
    fieldOf := func(expression ast.Expr) string {
        selector, isSelector := ast.Unparen(expression).(*ast.SelectorExpr)
        if false == isSelector {
            return ""
        }

        owner, isOwner := selector.X.(*ast.Ident)
        if false == isOwner {
            return ""
        }

        typeName, isConfig := configParameters[owner.Name]
        if false == isConfig {
            return ""
        }

        return typeName + "." + selector.Sel.Name
    }

    isZero := func(expression ast.Expr) bool {
        if value, isString := stringLiteralValue(expression); true == isString {
            return "" == value
        }

        identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)

        return true == isIdentifier && "nil" == identifier.Name
    }

    switch typed := ast.Unparen(condition).(type) {
    case *ast.BinaryExpr:
        if token.EQL != typed.Op {
            return ""
        }

        if true == isZero(typed.X) {
            return fieldOf(typed.Y)
        }

        if true == isZero(typed.Y) {
            return fieldOf(typed.X)
        }
    case *ast.CallExpr:
        selector, isSelector := typed.Fun.(*ast.SelectorExpr)
        if true == isSelector && "IsNilInterface" == selector.Sel.Name && 1 == len(typed.Args) {
            return fieldOf(typed.Args[0])
        }
    }

    return ""
}

/* namesAContextKey answers whether a refusal names what it refuses under "field" or "argument", which the walk over the keys reads */
func namesAContextKey(call *ast.CallExpr) bool {
    named := false
    ast.Inspect(call, func(node ast.Node) bool {
        pair, isPair := node.(*ast.KeyValueExpr)
        if false == isPair {
            return true
        }

        if key, isKey := stringLiteralValue(pair.Key); true == isKey && ("field" == key || "argument" == key) {
            named = true
        }

        return true
    })

    return named
}

func stringLiteralValue(expression ast.Expr) (string, bool) {
    literal, isLiteral := ast.Unparen(expression).(*ast.BasicLit)
    if false == isLiteral || token.STRING != literal.Kind {
        return "", false
    }

    value, unquoteErr := strconv.Unquote(literal.Value)
    if nil != unquoteErr {
        return "", false
    }

    return value, true
}

func TestFrameworkOnlySymbols_AreNotExported(t *testing.T) {
    removed := map[string]bool{}
    for _, symbol := range frameworkOnlySymbol {
        removed[symbol] = true
    }

    packages := map[string]bool{}
    declared := map[string]bool{}

    for _, source := range fourthMajorSourceList(t) {
        slashed := filepath.ToSlash(source)
        if true == strings.HasSuffix(slashed, "_test.go") || true == strings.HasPrefix(slashed, "../../../") {
            continue
        }

        fileSet := token.NewFileSet()
        file, parseErr := parser.ParseFile(fileSet, source, nil, 0)
        if nil != parseErr {
            t.Fatalf("parse %s: %v", source, parseErr)
        }

        key := packageKey(source)
        packages[key] = true

        for _, declaration := range file.Decls {
            if general, isGeneral := declaration.(*ast.GenDecl); true == isGeneral && token.VAR == general.Tok {
                for _, specification := range general.Specs {
                    for _, variableName := range specification.(*ast.ValueSpec).Names {
                        name := key + ":" + variableName.Name
                        declared[name] = true
                        if true == variableName.IsExported() && true == removed[name] {
                            t.Errorf("%s: %s is framework-only and left the surface; a variable does not re-export it", fileSet.Position(variableName.Pos()), name)
                        }
                    }
                }

                continue
            }

            function, isFunction := declaration.(*ast.FuncDecl)
            if false == isFunction {
                continue
            }

            name := key + ":" + function.Name.Name
            if nil != function.Recv && 0 < len(function.Recv.List) {
                name = key + ":" + baseTypeName(function.Recv.List[0].Type) + "." + function.Name.Name
            }

            declared[name] = true
            if true == function.Name.IsExported() && true == removed[name] {
                t.Errorf("%s: %s is framework-only and left the surface; keep it unexported", fileSet.Position(function.Pos()), name)
            }
        }
    }

    /* an entry pins something only while the unexported twin, or the successor named for it, is declared */
    for _, symbol := range frameworkOnlySymbol {
        packageName := strings.SplitN(symbol, ":", 2)[0]
        if false == packages[packageName] {
            t.Errorf("the framework-only entry %s names no package of the fourth major", symbol)

            continue
        }

        if successor, hasSuccessor := frameworkOnlySymbolSuccessor[symbol]; true == hasSuccessor {
            if false == declared[packageName+":"+successor] {
                t.Errorf("the framework-only entry %s names the successor %s, which is not declared", symbol, successor)
            }

            continue
        }

        if false == declared[unexportedTwin(symbol)] {
            t.Errorf("the framework-only entry %s has no unexported twin %s declared: drop the entry or name its successor", symbol, unexportedTwin(symbol))
        }
    }

    for symbol := range frameworkOnlySymbolSuccessor {
        if false == removed[symbol] {
            t.Errorf("the successor row %s names no framework-only entry", symbol)
        }
    }
}
