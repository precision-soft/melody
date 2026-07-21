package wiring

import (
    "path"
    "sort"
    "strconv"
    "strings"
    "unicode"
)

/* importAliasTable assigns every import path a unique alias. Two scanned packages routinely share a base name — a "contract" package under each domain — so the alias written into the generated file cannot simply be the package name. */
func newImportAliasTable() *importAliasTable {
    return &importAliasTable{
        aliasByPath: make(map[string]string),
        takenAlias:  make(map[string]bool),
    }
}

type importAliasTable struct {
    aliasByPath map[string]string
    takenAlias  map[string]bool
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
    if existingAlias, exists := instance.aliasByPath[importPath]; true == exists {
        return existingAlias
    }

    candidate := sanitizeAlias(path.Base(importPath))

    alias := candidate
    for suffix := 2; true == instance.takenAlias[alias]; suffix = suffix + 1 {
        alias = candidate + strconv.Itoa(suffix)
    }

    instance.aliasByPath[importPath] = alias
    instance.takenAlias[alias] = true

    return alias
}

func (instance *importAliasTable) sortedPaths() []string {
    paths := make([]string, 0, len(instance.aliasByPath))

    for importPath := range instance.aliasByPath {
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

func renderProvider(
    constructor *Constructor,
    resolvedArguments []*resolvedArgument,
    importAliases *importAliasTable,
) string {
    returnTypeExpression := renderType(constructor.ReturnType, importAliases)
    constructorExpression := importAliases.alias(constructor.ImportPath) + "." + constructor.Name

    var builder strings.Builder

    builder.WriteString(indent)
    builder.WriteString(containerImportAlias)

    /* a constructor whose package declares a service name is registered under it as well as under its type, so the name-based lookups the package already exposes keep resolving; the name is referenced as the constant rather than copied, leaving its definition in one place */
    if "" == constructor.ServiceNameIdentifier {
        builder.WriteString(".MustRegisterType(\n")
        builder.WriteString(indent + indent + "registrar,\n")
    } else {
        builder.WriteString(".MustRegister(\n")
        builder.WriteString(indent + indent + "registrar,\n")
        builder.WriteString(indent + indent + importAliases.alias(constructor.ImportPath) + "." + constructor.ServiceNameIdentifier + ",\n")
    }
    builder.WriteString(indent + indent + "func(resolver " + containerContractImportAlias + ".Resolver) (" + returnTypeExpression + ", error) {\n")

    body := indent + indent + indent

    requiresConfiguration := false
    for _, resolved := range resolvedArguments {
        if true == resolved.argument.IsScalar {
            requiresConfiguration = true

            break
        }
    }

    if true == requiresConfiguration {
        builder.WriteString(body + "configuration := " + configImportAlias + ".ConfigMustFromResolver(resolver)\n\n")
    }

    for _, resolved := range resolvedArguments {
        builder.WriteString(renderArgument(resolved, importAliases, body))
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
) string {
    var builder strings.Builder

    errorVariable := resolved.variableName + "Err"

    if false == resolved.argument.IsScalar {
        builder.WriteString(
            body + resolved.variableName + ", " + errorVariable + " := " +
                containerImportAlias + ".FromResolverByType[" + renderType(resolved.argument.Type, importAliases) + "](resolver)\n",
        )
        builder.WriteString(body + "if nil != " + errorVariable + " {\n")
        builder.WriteString(body + indent + "return nil, " + errorVariable + "\n")
        builder.WriteString(body + "}\n\n")

        return builder.String()
    }

    accessor, isFallible := scalarAccessor(resolved.argument.Type.Expression)

    parameterExpression := "configuration.MustGet(" + strconv.Quote(resolved.parameterName) + ")"

    if false == isFallible {
        builder.WriteString(body + resolved.variableName + " := " + parameterExpression + "." + accessor + "\n\n")

        return builder.String()
    }

    /* the accessor returns the widest type of its family, so only a narrower argument needs a conversion; naming the variable directly otherwise keeps the generated provider free of a pointless intermediate */
    if false == scalarNeedsConversion(resolved.argument.Type.Expression) {
        builder.WriteString(body + resolved.variableName + ", " + errorVariable + " := " + parameterExpression + "." + accessor + "\n")
        builder.WriteString(body + "if nil != " + errorVariable + " {\n")
        builder.WriteString(body + indent + "return nil, " + errorVariable + "\n")
        builder.WriteString(body + "}\n\n")

        return builder.String()
    }

    builder.WriteString(body + resolved.variableName + "Value, " + errorVariable + " := " + parameterExpression + "." + accessor + "\n")
    builder.WriteString(body + "if nil != " + errorVariable + " {\n")
    builder.WriteString(body + indent + "return nil, " + errorVariable + "\n")
    builder.WriteString(body + "}\n\n")
    builder.WriteString(body + resolved.variableName + " := " + resolved.argument.Type.Expression + "(" + resolved.variableName + "Value)\n\n")

    return builder.String()
}

/* scalarAccessor maps a Go type onto the parameter accessor that reads it, and reports whether that accessor returns an error. A string is read through MustString: the generator has already verified the parameter is declared, so the only remaining failure is a declaration holding a non-string, which is a wiring mistake rather than a runtime condition. */
func scalarAccessor(typeExpression string) (string, bool) {
    switch typeExpression {
    case "string":
        return "MustString()", false
    case "bool":
        return "Bool()", true
    case "float32", "float64":
        return "Float()", true
    case "time.Duration":
        return "Duration()", true
    default:
        return "Int()", true
    }
}

func scalarNeedsConversion(typeExpression string) bool {
    switch typeExpression {
    case "string", "bool", "int", "float64", "time.Duration":
        return false
    default:
        return true
    }
}

func renderFile(
    packageName string,
    functionName string,
    importAliases *importAliasTable,
    providerBlocks []string,
    requiresConfiguration bool,
) string {
    var builder strings.Builder

    builder.WriteString(generatedFileNote + "\n\n")
    builder.WriteString("package " + packageName + "\n\n")

    builder.WriteString("import (\n")

    for _, importPath := range importAliases.sortedPaths() {
        builder.WriteString(indent + importAliases.alias(importPath) + " " + strconv.Quote(importPath) + "\n")
    }

    builder.WriteString(")\n\n")

    builder.WriteString("/* " + functionName + " registers every service discovered under the scanned packages. */\n")
    builder.WriteString("func " + functionName + "(registrar " + containerContractImportAlias + ".Registrar) {\n")

    if 0 == len(providerBlocks) {
        builder.WriteString("}\n")

        return builder.String()
    }

    builder.WriteString(strings.Join(providerBlocks, "\n"))
    builder.WriteString("}\n")

    return builder.String()
}
