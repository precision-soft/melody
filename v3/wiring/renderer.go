package wiring

import (
    "go/token"
    "path"
    "sort"
    "strconv"
    "strings"
    "unicode"
)

/* the provider bodies spell these identifiers — the closure parameters, the fixed locals, the conversion type names, the scalar element types a pointer argument renders verbatim, the error of every signature, the string and any of every guard's error context, and the nil/true/false literals; an import alias or a generated local claiming one would shadow it inside every closure, so the alias suffix loop and the variable namer both step over them. */
var emittedBodyIdentifiers = []string{
    "resolver",
    "registrar",
    "configuration",
    "zeroValue",
    "bool",
    "int",
    "int8",
    "int16",
    "int32",
    "int64",
    "uint",
    "uint8",
    "uint16",
    "uint32",
    "uint64",
    "byte",
    "rune",
    "float32",
    "float64",
    "string",
    "any",
    "error",
    "nil",
    "true",
    "false",
}

/* importAliasTable assigns every import path a unique alias. Two scanned packages routinely share a base name — a "contract" package under each domain — so the alias written into the generated file cannot simply be the package name. A path is reserved so its alias is fixed before any other path can claim it, but only a path an emitted expression actually asked for (through alias) is written into the import block: an unused import does not compile. */
func newImportAliasTable() *importAliasTable {
    table := &importAliasTable{
        aliasByPath: make(map[string]string),
        takenAlias:  make(map[string]bool),
        usedPaths:   make(map[string]bool),
    }

    for _, emittedIdentifier := range emittedBodyIdentifiers {
        table.takenAlias[emittedIdentifier] = true
    }

    return table
}

/* reserveIdentifier withholds a bare identifier — the generated function's own name — from the alias suffix loop without binding it to an import path. */
func (instance *importAliasTable) reserveIdentifier(identifier string) {
    instance.takenAlias[identifier] = true
}

type importAliasTable struct {
    aliasByPath map[string]string
    takenAlias  map[string]bool
    usedPaths   map[string]bool
}

func (instance *importAliasTable) reserve(importPath string, alias string) string {
    if existingAlias, exists := instance.aliasByPath[importPath]; true == exists {
        return existingAlias
    }

    instance.aliasByPath[importPath] = alias
    instance.takenAlias[alias] = true

    return alias
}

func (instance *importAliasTable) alias(importPath string) string {
    instance.usedPaths[importPath] = true

    if existingAlias, exists := instance.aliasByPath[importPath]; true == exists {
        return existingAlias
    }

    candidate := sanitizeAlias(path.Base(importPath))

    /* a directory may be named after a Go keyword (app/interface); the keyword check pushes the alias into the suffixed form the import clause can parse */
    alias := candidate
    for suffix := 2; true == instance.takenAlias[alias] || true == token.IsKeyword(alias); suffix = suffix + 1 {
        alias = candidate + strconv.Itoa(suffix)
    }

    instance.aliasByPath[importPath] = alias
    instance.takenAlias[alias] = true

    return alias
}

func (instance *importAliasTable) sortedUsedPaths() []string {
    paths := make([]string, 0, len(instance.usedPaths))

    for importPath := range instance.usedPaths {
        paths = append(paths, importPath)
    }

    sort.Strings(paths)

    return paths
}

func sanitizeAlias(value string) string {
    var builder strings.Builder

    for _, character := range value {
        if true == unicode.IsLetter(character) || true == unicode.IsDigit(character) || '_' == character {
            builder.WriteRune(character)

            continue
        }

        builder.WriteRune('_')
    }

    alias := builder.String()
    if "" == alias {
        return "pkg"
    }

    if true == unicode.IsDigit(rune(alias[0])) {
        return "pkg" + alias
    }

    return alias
}

/* renderType rewrites a type as the generated file must spell it, swapping the qualifier the source used for the alias the generated file imports it under. */
func renderType(typeReference *TypeReference, importAliases *importAliasTable) string {
    if "" == typeReference.ImportPath {
        return typeReference.Expression
    }

    alias := importAliases.alias(typeReference.ImportPath)

    prefix := ""
    expression := typeReference.Expression

    for true == strings.HasPrefix(expression, "*") {
        prefix = prefix + "*"
        expression = strings.TrimPrefix(expression, "*")
    }

    return prefix + alias + "." + strings.TrimPrefix(expression, typeReference.Qualifier+".")
}

/* assignVariableNames renames the generated locals away from everything else the provider body references — the closure parameters, the fixed helper names, the import aliases of the types it spells, and one another's derived Err/Value forms. An argument named after its package qualifier (repository repository.ProductRepository) is idiomatic Go and would otherwise shadow the alias. */
func assignVariableNames(
    constructor *Constructor,
    resolvedArguments []*resolvedArgument,
    importAliases *importAliasTable,
) {
    forbidden := map[string]bool{
        containerContractImportAlias: true,
        containerImportAlias:         true,
        configImportAlias:            true,
        exceptionImportAlias:         true,
        mathImportAlias:              true,
    }

    for _, emittedIdentifier := range emittedBodyIdentifiers {
        forbidden[emittedIdentifier] = true
    }

    forbidden[importAliases.alias(constructor.ImportPath)] = true

    if "" != constructor.ReturnType.ImportPath {
        forbidden[importAliases.alias(constructor.ReturnType.ImportPath)] = true
    }

    for _, resolved := range resolvedArguments {
        /* a scalar never renders its type, so asking for its alias would drag an unused import into the file */
        if true == resolved.argument.IsScalar {
            continue
        }

        if "" != resolved.argument.Type.ImportPath {
            forbidden[importAliases.alias(resolved.argument.Type.ImportPath)] = true
        }
    }

    for _, resolved := range resolvedArguments {
        candidate := resolved.argument.Name

        for suffix := 2; true == forbidden[candidate] || true == forbidden[candidate+"Err"] || true == forbidden[candidate+"Value"]; suffix = suffix + 1 {
            candidate = resolved.argument.Name + strconv.Itoa(suffix)
        }

        resolved.variableName = candidate

        forbidden[candidate] = true
        forbidden[candidate+"Err"] = true
        forbidden[candidate+"Value"] = true
    }
}

func renderProvider(
    constructor *Constructor,
    resolvedArguments []*resolvedArgument,
    importAliases *importAliasTable,
) string {
    assignVariableNames(constructor, resolvedArguments, importAliases)

    returnTypeExpression := renderType(constructor.ReturnType, importAliases)
    constructorExpression := importAliases.alias(constructor.ImportPath) + "." + constructor.Name

    containerAlias := importAliases.alias(containerImportPath)
    containerContractAlias := importAliases.alias(containerContractImportPath)

    var builder strings.Builder

    builder.WriteString(indent)
    builder.WriteString(containerAlias)

    /* the lifetime is spelled in the verb, exactly as a hand-written registration spells it: a scoped constructor registers through MustRegisterScoped and is built once per scope, and the two registrar interfaces share no method, so the emitted call only compiles against the registrar its own function is handed */
    registerByType := ".MustRegisterType(\n"
    registerByName := ".MustRegister(\n"
    if true == constructor.IsScoped {
        registerByType = ".MustRegisterScopedType(\n"
        registerByName = ".MustRegisterScoped(\n"
    }

    /* a constructor whose package declares a service name is registered under it as well as under its type, so the name-based lookups the package already exposes keep resolving; the name is referenced as the constant rather than copied, leaving its definition in one place */
    if "" == constructor.ServiceNameIdentifier {
        builder.WriteString(registerByType)
        builder.WriteString(indent + indent + "registrar,\n")
    } else {
        builder.WriteString(registerByName)
        builder.WriteString(indent + indent + "registrar,\n")
        builder.WriteString(indent + indent + importAliases.alias(constructor.ImportPath) + "." + constructor.ServiceNameIdentifier + ",\n")
    }
    builder.WriteString(indent + indent + "func(resolver " + containerContractAlias + ".Resolver) (" + returnTypeExpression + ", error) {\n")

    body := indent + indent + indent

    requiresConfiguration := false
    hasFallibleArgument := false
    for _, resolved := range resolvedArguments {
        if true == resolved.argument.IsScalar {
            requiresConfiguration = true
        } else {
            hasFallibleArgument = true
        }

        if true == resolved.argument.IsScalar {
            if _, isFallible := scalarAccessor(resolved.argument.Type); true == isFallible {
                hasFallibleArgument = true
            }
        }
    }

    /* nil is not a value of a struct or of a named scalar type, and the source alone cannot tell a non-pointer named type apart from an interface, so an error path out of a non-pointer provider returns a declared zero value, which is valid for every kind of type */
    errorReturnExpression := "nil"
    if true == hasFallibleArgument && false == constructor.ReturnType.IsPointer {
        errorReturnExpression = "zeroValue"

        builder.WriteString(body + "var zeroValue " + returnTypeExpression + "\n\n")
    }

    if true == requiresConfiguration {
        builder.WriteString(body + "configuration := " + importAliases.alias(configImportPath) + ".ConfigMustFromResolver(resolver)\n\n")
    }

    for _, resolved := range resolvedArguments {
        builder.WriteString(renderArgument(resolved, importAliases, body, errorReturnExpression))
    }

    callArguments := make([]string, 0, len(resolvedArguments))
    for _, resolved := range resolvedArguments {
        callArguments = append(callArguments, resolved.variableName)
    }

    builder.WriteString(body + "return " + constructorExpression + "(")

    if 0 == len(callArguments) {
        builder.WriteString(")")
    } else {
        builder.WriteString("\n")

        for _, callArgument := range callArguments {
            builder.WriteString(body + indent + callArgument + ",\n")
        }

        builder.WriteString(body + ")")
    }

    if true == constructor.ReturnsError {
        builder.WriteString("\n")
    } else {
        builder.WriteString(", nil\n")
    }

    builder.WriteString(indent + indent + "},\n")
    builder.WriteString(indent + ")\n")

    return builder.String()
}

func renderArgument(
    resolved *resolvedArgument,
    importAliases *importAliasTable,
    body string,
    errorReturnExpression string,
) string {
    var builder strings.Builder

    errorVariable := resolved.variableName + "Err"

    if false == resolved.argument.IsScalar {
        builder.WriteString(
            body + resolved.variableName + ", " + errorVariable + " := " +
                importAliases.alias(containerImportPath) + ".FromResolverByType[" + renderType(resolved.argument.Type, importAliases) + "](resolver)\n",
        )
        builder.WriteString(body + "if nil != " + errorVariable + " {\n")
        builder.WriteString(body + indent + "return " + errorReturnExpression + ", " + errorVariable + "\n")
        builder.WriteString(body + "}\n\n")

        return builder.String()
    }

    accessor, isFallible := scalarAccessor(resolved.argument.Type)

    parameterExpression := "configuration.MustGet(" + strconv.Quote(resolved.parameterName) + ")"

    if false == isFallible {
        builder.WriteString(body + resolved.variableName + " := " + parameterExpression + "." + accessor + "\n\n")

        return builder.String()
    }

    /* the accessor returns the widest type of its family, so only a narrower argument needs a conversion; naming the variable directly otherwise keeps the generated provider free of a pointless intermediate */
    if false == scalarNeedsConversion(resolved.argument.Type) {
        builder.WriteString(body + resolved.variableName + ", " + errorVariable + " := " + parameterExpression + "." + accessor + "\n")
        builder.WriteString(body + "if nil != " + errorVariable + " {\n")
        builder.WriteString(body + indent + "return " + errorReturnExpression + ", " + errorVariable + "\n")
        builder.WriteString(body + "}\n\n")

        return builder.String()
    }

    builder.WriteString(body + resolved.variableName + "Value, " + errorVariable + " := " + parameterExpression + "." + accessor + "\n")
    builder.WriteString(body + "if nil != " + errorVariable + " {\n")
    builder.WriteString(body + indent + "return " + errorReturnExpression + ", " + errorVariable + "\n")
    builder.WriteString(body + "}\n\n")

    builder.WriteString(renderNarrowingGuard(resolved, importAliases, body, errorReturnExpression))

    builder.WriteString(body + resolved.variableName + " := " + resolved.argument.Type.Expression + "(" + resolved.variableName + "Value)\n\n")

    return builder.String()
}

/* renderNarrowingGuard emits the range check in front of a conversion that narrows the accessor's widest type. A Go conversion wraps silently, so a parameter of -1 handed to a uint argument would become the largest value the type holds — the guard turns it into an error naming the parameter and the argument instead. */
func renderNarrowingGuard(
    resolved *resolvedArgument,
    importAliases *importAliasTable,
    body string,
    errorReturnExpression string,
) string {
    narrowingRange, hasRange := scalarNarrowingRange(resolved.argument.Type.Expression)
    if false == hasRange {
        return ""
    }

    valueVariable := resolved.variableName + "Value"

    condition := valueVariable + " < " + narrowingRange.lowerBound
    if "" != narrowingRange.upperBound {
        comparedValue := valueVariable
        if "" != narrowingRange.upperBoundValueConversion {
            comparedValue = narrowingRange.upperBoundValueConversion + "(" + valueVariable + ")"
        }

        condition = condition + " || " + narrowingRange.upperBound + " < " + comparedValue
    }

    if true == strings.Contains(condition, mathImportAlias+".") {
        _ = importAliases.alias(mathImportPath)
    }

    var builder strings.Builder

    builder.WriteString(body + "if " + condition + " {\n")
    builder.WriteString(body + indent + "return " + errorReturnExpression + ", " + importAliases.alias(exceptionImportPath) + ".NewError(\n")
    builder.WriteString(body + indent + indent + strconv.Quote("a configuration parameter does not fit the constructor argument") + ",\n")
    builder.WriteString(body + indent + indent + "map[string]any{\n")
    builder.WriteString(body + indent + indent + indent + strconv.Quote("parameter") + ": " + strconv.Quote(resolved.parameterName) + ",\n")
    builder.WriteString(body + indent + indent + indent + strconv.Quote("argument") + ": " + strconv.Quote(resolved.argument.Name) + ",\n")
    builder.WriteString(body + indent + indent + indent + strconv.Quote("argumentType") + ": " + strconv.Quote(resolved.argument.Type.Expression) + ",\n")
    builder.WriteString(body + indent + indent + "},\n")
    builder.WriteString(body + indent + indent + "nil,\n")
    builder.WriteString(body + indent + ")\n")
    builder.WriteString(body + "}\n\n")

    return builder.String()
}

type scalarNarrowingBounds struct {
    lowerBound string
    upperBound string
    /* math.MaxUint32 overflows a 32-bit int, so that comparison runs through int64 */
    upperBoundValueConversion string
}

/* scalarNarrowingRange yields the bounds of a scalar type narrower than the widest type its accessor returns. An empty upper bound means the accessor value cannot exceed it, so only the lower one is checked. The float32 bounds catch the magnitudes a conversion would silently turn into an infinity. */
func scalarNarrowingRange(typeExpression string) (*scalarNarrowingBounds, bool) {
    mathQualifier := mathImportAlias + "."

    switch typeExpression {
    case "int8":
        return &scalarNarrowingBounds{lowerBound: mathQualifier + "MinInt8", upperBound: mathQualifier + "MaxInt8"}, true
    case "int16":
        return &scalarNarrowingBounds{lowerBound: mathQualifier + "MinInt16", upperBound: mathQualifier + "MaxInt16"}, true
    case "int32", "rune":
        return &scalarNarrowingBounds{lowerBound: mathQualifier + "MinInt32", upperBound: mathQualifier + "MaxInt32"}, true
    case "uint8", "byte":
        return &scalarNarrowingBounds{lowerBound: "0", upperBound: mathQualifier + "MaxUint8"}, true
    case "uint16":
        return &scalarNarrowingBounds{lowerBound: "0", upperBound: mathQualifier + "MaxUint16"}, true
    case "uint32":
        return &scalarNarrowingBounds{lowerBound: "0", upperBound: mathQualifier + "MaxUint32", upperBoundValueConversion: "int64"}, true
    case "uint", "uint64":
        return &scalarNarrowingBounds{lowerBound: "0", upperBound: ""}, true
    case "float32":
        return &scalarNarrowingBounds{lowerBound: "-" + mathQualifier + "MaxFloat32", upperBound: mathQualifier + "MaxFloat32"}, true
    default:
        return nil, false
    }
}

/* scalarAccessor maps a scalar argument onto the parameter accessor that reads it, and reports whether that accessor returns an error. The stdlib duration is recognized by the import path its qualifier resolves to, so an aliased time import keeps its accessor. A string is read through MustString: the generator has already verified the parameter is declared, so the only remaining failure is a declaration holding a non-string, which is a wiring mistake rather than a runtime condition. */
func scalarAccessor(typeReference *TypeReference) (string, bool) {
    if true == isStandardDuration(typeReference) {
        return "Duration()", true
    }

    switch typeReference.Expression {
    case "string":
        return "MustString()", false
    case "bool":
        return "Bool()", true
    case "float32", "float64":
        return "Float()", true
    default:
        return "Int()", true
    }
}

func scalarNeedsConversion(typeReference *TypeReference) bool {
    if true == isStandardDuration(typeReference) {
        return false
    }

    switch typeReference.Expression {
    case "string", "bool", "int", "float64":
        return false
    default:
        return true
    }
}

func renderFile(
    packageName string,
    functionName string,
    scopedFunctionName string,
    importAliases *importAliasTable,
    providerBlocks []string,
    scopedProviderBlocks []string,
) string {
    /* the signature references the contract package whatever the scan found, so it is asked for before the import block is written */
    containerContractAlias := importAliases.alias(containerContractImportPath)

    var builder strings.Builder

    builder.WriteString(generatedFileNote + "\n\n")
    builder.WriteString("package " + packageName + "\n\n")

    builder.WriteString("import (\n")

    for _, importPath := range importAliases.sortedUsedPaths() {
        builder.WriteString(indent + importAliases.alias(importPath) + " " + strconv.Quote(importPath) + "\n")
    }

    builder.WriteString(")\n\n")

    builder.WriteString("/* " + functionName + " registers every service discovered under the scanned packages. */\n")
    builder.WriteString("func " + functionName + "(registrar " + containerContractAlias + ".Registrar) {\n")

    if 0 == len(providerBlocks) {
        builder.WriteString("}\n")
    } else {
        builder.WriteString(strings.Join(providerBlocks, "\n"))
        builder.WriteString("}\n")
    }

    /* the scoped function is emitted only when something is scoped: the two registrars arrive at two different module hooks and neither satisfies the other, so one combined function could not be called from either, and a project that declares nothing scoped must keep regenerating the file it had */
    if 0 == len(scopedProviderBlocks) {
        return builder.String()
    }

    builder.WriteString("\n")
    builder.WriteString("/* " + scopedFunctionName + " registers every scope-owned service discovered under the scanned packages. What it registers is built once per scope and closed when that scope closes. */\n")
    builder.WriteString("func " + scopedFunctionName + "(registrar " + containerContractAlias + ".ScopedRegistrar) {\n")
    builder.WriteString(strings.Join(scopedProviderBlocks, "\n"))
    builder.WriteString("}\n")

    return builder.String()
}
