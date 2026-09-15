package wiring

import (
    "go/ast"
    "go/build"
    "go/parser"
    "go/token"
    "io/fs"
    "path"
    "path/filepath"
    "sort"
    "strconv"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
)

const (
    constructorPrefix = "New"
    directivePrefix   = "//melody:"
    bindDirective     = "//melody:bind"
    ignoreDirective   = "//melody:ignore"
    serviceDirective  = "//melody:service"
    scopedDirective   = "//melody:scoped"
)

/* Constructor is one discovered provider function, described in the terms the generator emits: where it lives, what it returns and what it needs. */
type Constructor struct {
    Name        string
    ImportPath  string
    PackageName string
    File        string
    Line        int
    ReturnType  *TypeReference
    /* ServiceNameIdentifier is the exported constant a //melody:service directive names. The generated file references the constant rather than copying its value, so the service name keeps a single definition. Empty registers the service by type alone. */
    ServiceNameIdentifier string
    /* IsScoped marks a constructor a //melody:scoped directive declares as request-lifetime. It is emitted into the scoped registration function instead of the container one, so the service is built once per scope and closed with it. */
    IsScoped       bool
    ReturnsError   bool
    Arguments      []*Argument
    DirectiveBinds map[string]string
}

/* Argument is one constructor parameter. A scalar is filled from a configuration parameter through a bind; anything else is resolved from the container by type. */
type Argument struct {
    Name     string
    Type     *TypeReference
    IsScalar bool
}

/* TypeReference is a type as written at the constructor, together with the import path its qualifier resolves to, so the generated file can render it from a different package. */
type TypeReference struct {
    Expression string
    Qualifier  string
    ImportPath string
    IsPointer  bool
}

var scalarTypeNames = map[string]bool{
    "string":  true,
    "bool":    true,
    "int":     true,
    "int8":    true,
    "int16":   true,
    "int32":   true,
    "int64":   true,
    "uint":    true,
    "uint8":   true,
    "uint16":  true,
    "uint32":  true,
    "uint64":  true,
    "float32": true,
    "float64": true,
    "byte":    true,
    "rune":    true,
}

/* ScanResult carries what a scan found together with what it deliberately left out, so the generator can report the skipped constructors instead of silently narrowing its coverage. */
type ScanResult struct {
    Constructors []*Constructor
    Skipped      []*SkippedConstructor
    /* SkippedVendorDirectories names the vendor trees the walk stepped over; they cannot contribute services, so they are reported separately from the skipped constructors strict fails on. */
    SkippedVendorDirectories []string
    /* ExcludedFiles lists constructor-bearing files omitted by build constraints. Supply the matching build tags to include them; strict mode cannot infer the binary’s tags. */
    ExcludedFiles []string
    /* UnusedExcludes lists unmatched exclusion patterns in declaration order. */
    UnusedExcludes []string
}

type SkippedConstructor struct {
    Name   string
    File   string
    Line   int
    Reason string
}

/* Scan discovers constructors in the declared directory and descendant packages, deriving import paths from relative paths. A symlinked root is resolved before walking; symlinked subdirectories are not followed. */
func Scan(projectDirectory string, packageBinding *PackageBinding, buildTags []string) (*ScanResult, error) {
    rootDirectory := packageBinding.Directory()
    if false == filepath.IsAbs(rootDirectory) {
        rootDirectory = filepath.Join(projectDirectory, rootDirectory)
    }

    resolvedRootDirectory, resolveErr := filepath.EvalSymlinks(rootDirectory)
    if nil != resolveErr {
        return nil, exception.NewError(
            "could not resolve the package directory",
            map[string]any{
                "directory":  rootDirectory,
                "importPath": packageBinding.ImportPath(),
            },
            resolveErr,
        )
    }
    rootDirectory = resolvedRootDirectory

    excludes := packageBinding.Excludes()
    for _, pattern := range excludes {
        if _, matchErr := path.Match(pattern, ""); nil != matchErr {
            return nil, exception.NewError(
                "an exclude pattern is malformed",
                map[string]any{
                    "pattern":    pattern,
                    "importPath": packageBinding.ImportPath(),
                },
                matchErr,
            )
        }
    }

    result := &ScanResult{
        Constructors: make([]*Constructor, 0),
        Skipped:      make([]*SkippedConstructor, 0),
    }

    matchedExcludes := make(map[string]bool)

    fileSet := token.NewFileSet()

    walkErr := filepath.WalkDir(rootDirectory, func(currentPath string, entry fs.DirEntry, walkErr error) error {
        if nil != walkErr {
            return walkErr
        }

        if true == entry.IsDir() {
            if currentPath == rootDirectory {
                return nil
            }

            baseName := entry.Name()
            if "vendor" == baseName {
                result.SkippedVendorDirectories = append(result.SkippedVendorDirectories, currentPath)

                return filepath.SkipDir
            }

            if true == strings.HasPrefix(baseName, ".") || true == strings.HasPrefix(baseName, "_") || "testdata" == baseName {
                return filepath.SkipDir
            }

            return nil
        }

        if false == strings.HasSuffix(currentPath, ".go") || true == strings.HasSuffix(currentPath, "_test.go") {
            return nil
        }

        return scanFile(fileSet, rootDirectory, currentPath, packageBinding, buildTags, matchedExcludes, result)
    })
    if nil != walkErr {
        return nil, exception.NewError(
            "could not scan the package directory",
            map[string]any{
                "directory":  rootDirectory,
                "importPath": packageBinding.ImportPath(),
            },
            walkErr,
        )
    }

    for _, pattern := range excludes {
        if false == matchedExcludes[pattern] {
            result.UnusedExcludes = append(result.UnusedExcludes, pattern)
        }
    }

    sort.SliceStable(result.Constructors, func(first int, second int) bool {
        if result.Constructors[first].ImportPath != result.Constructors[second].ImportPath {
            return result.Constructors[first].ImportPath < result.Constructors[second].ImportPath
        }

        return result.Constructors[first].Name < result.Constructors[second].Name
    })

    return result, nil
}

func scanFile(
    fileSet *token.FileSet,
    rootDirectory string,
    currentPath string,
    packageBinding *PackageBinding,
    buildTags []string,
    matchedExcludes map[string]bool,
    result *ScanResult,
) error {

    buildContext := build.Default
    buildContext.BuildTags = buildTags
    isIncluded, matchErr := buildContext.MatchFile(filepath.Dir(currentPath), filepath.Base(currentPath))
    if nil != matchErr {
        return exception.NewError(
            "could not evaluate the build constraints of a source file",
            map[string]any{
                "file": currentPath,
            },
            matchErr,
        )
    }

    if false == isIncluded {

        if true == fileHasConstructorCandidate(fileSet, currentPath) {
            result.ExcludedFiles = append(result.ExcludedFiles, currentPath)
        }

        return nil
    }

    fileNode, parseErr := parser.ParseFile(fileSet, currentPath, nil, parser.ParseComments)
    if nil != parseErr {
        return exception.NewError(
            "could not parse a source file",
            map[string]any{
                "file": currentPath,
            },
            parseErr,
        )
    }

    if "main" == fileNode.Name.Name {
        for _, declaration := range fileNode.Decls {
            functionDeclaration, isFunction := declaration.(*ast.FuncDecl)
            if false == isFunction || false == isConstructorCandidate(functionDeclaration) {
                continue
            }

            directives, directivesErr := parseDirectives(fileSet, currentPath, functionDeclaration)
            if nil != directivesErr {
                return directivesErr
            }

            if true == directives.isIgnored {
                continue
            }

            position := fileSet.Position(functionDeclaration.Pos())

            result.Skipped = append(result.Skipped, &SkippedConstructor{
                Name:   functionDeclaration.Name.Name,
                File:   currentPath,
                Line:   position.Line,
                Reason: "declared in a main package the generated file cannot import",
            })
        }

        return nil
    }

    importPath, importPathErr := derivedImportPath(packageBinding.ImportPath(), rootDirectory, currentPath)
    if nil != importPathErr {
        return importPathErr
    }

    fileImports := collectImports(fileNode)

    for _, declaration := range fileNode.Decls {
        functionDeclaration, isFunction := declaration.(*ast.FuncDecl)
        if false == isFunction {
            continue
        }

        if false == isConstructorCandidate(functionDeclaration) {
            continue
        }

        position := fileSet.Position(functionDeclaration.Pos())

        directives, directivesErr := parseDirectives(fileSet, currentPath, functionDeclaration)
        if nil != directivesErr {
            return directivesErr
        }

        if true == directives.isIgnored {
            continue
        }

        constructor, skipReason := describeConstructor(
            functionDeclaration,
            importPath,
            fileNode.Name.Name,
            fileImports,
            directives,
        )

        if "" != skipReason {
            result.Skipped = append(result.Skipped, &SkippedConstructor{
                Name:   functionDeclaration.Name.Name,
                File:   currentPath,
                Line:   position.Line,
                Reason: skipReason,
            })

            continue
        }

        if true == isExcluded(constructor.ReturnType.Expression, packageBinding.Excludes(), matchedExcludes) {
            continue
        }

        constructor.File = currentPath
        constructor.Line = position.Line

        result.Constructors = append(result.Constructors, constructor)
    }

    return nil
}

func fileHasConstructorCandidate(fileSet *token.FileSet, currentPath string) bool {
    fileNode, parseErr := parser.ParseFile(fileSet, currentPath, nil, parser.ParseComments)
    if nil != parseErr {
        return false
    }

    if "main" == fileNode.Name.Name {
        return false
    }

    for _, declaration := range fileNode.Decls {
        functionDeclaration, isFunction := declaration.(*ast.FuncDecl)
        if false == isFunction {
            continue
        }

        if true == isConstructorCandidate(functionDeclaration) {
            return true
        }
    }

    return false
}

func isConstructorCandidate(functionDeclaration *ast.FuncDecl) bool {
    if nil != functionDeclaration.Recv {
        return false
    }

    if false == functionDeclaration.Name.IsExported() {
        return false
    }

    if false == strings.HasPrefix(functionDeclaration.Name.Name, constructorPrefix) {
        return false
    }

    return true
}

type constructorDirectives struct {
    binds                 map[string]string
    serviceNameIdentifier string
    isIgnored             bool
    isScoped              bool
}

func parseDirectives(
    fileSet *token.FileSet,
    currentPath string,
    functionDeclaration *ast.FuncDecl,
) (*constructorDirectives, error) {
    directives := &constructorDirectives{
        binds: make(map[string]string),
    }

    if nil == functionDeclaration.Doc {
        return directives, nil
    }

    for _, comment := range functionDeclaration.Doc.List {
        text := strings.TrimSpace(comment.Text)

        if _, isIgnore := directiveRemainder(text, ignoreDirective); true == isIgnore {
            directives.isIgnored = true

            continue
        }

        if _, isScoped := directiveRemainder(text, scopedDirective); true == isScoped {
            directives.isScoped = true

            continue
        }

        if remainder, isService := directiveRemainder(text, serviceDirective); true == isService {
            fields := strings.Fields(remainder)
            if 0 < len(fields) {
                directives.serviceNameIdentifier = fields[0]
            }

            continue
        }

        if remainder, isBind := directiveRemainder(text, bindDirective); true == isBind {
            for _, assignment := range strings.Fields(remainder) {
                separatorIndex := strings.Index(assignment, "=")

                if 0 >= separatorIndex || len(assignment)-1 == separatorIndex {
                    return nil, exception.NewError(
                        "a bind directive assignment must be spelled argument=parameter",
                        map[string]any{
                            "assignment":  assignment,
                            "constructor": functionDeclaration.Name.Name,
                            "file":        currentPath,
                            "line":        fileSet.Position(comment.Pos()).Line,
                        },
                        nil,
                    )
                }

                directives.binds[assignment[:separatorIndex]] = assignment[separatorIndex+1:]
            }

            continue
        }

        if true == strings.HasPrefix(text, directivePrefix) {
            return nil, exception.NewError(
                "an unknown melody directive is not one of bind, ignore, service or scoped",
                map[string]any{
                    "directive":   text,
                    "constructor": functionDeclaration.Name.Name,
                    "file":        currentPath,
                    "line":        fileSet.Position(comment.Pos()).Line,
                },
                nil,
            )
        }
    }

    return directives, nil
}

func directiveRemainder(text string, directive string) (string, bool) {
    if text == directive {
        return "", true
    }

    if true == strings.HasPrefix(text, directive+" ") || true == strings.HasPrefix(text, directive+"\t") {
        return text[len(directive)+1:], true
    }

    return "", false
}

func describeConstructor(
    functionDeclaration *ast.FuncDecl,
    importPath string,
    packageName string,
    fileImports map[string]string,
    directives *constructorDirectives,
) (*Constructor, string) {
    if nil != functionDeclaration.Type.TypeParams && 0 < len(functionDeclaration.Type.TypeParams.List) {
        return nil, "the constructor is generic, so the type arguments cannot be derived from the source"
    }

    results := functionDeclaration.Type.Results
    if nil == results || 0 == len(results.List) {
        return nil, "the constructor returns nothing"
    }

    returnCount := 0
    for _, result := range results.List {
        returnCount = returnCount + max(1, len(result.Names))
    }

    if 2 < returnCount {
        return nil, "the constructor returns more than a value and an error"
    }

    returnType := describeType(results.List[0].Type, importPath, packageName, fileImports)
    if nil == returnType {
        return nil, "the returned type is not a named type"
    }

    if "error" == returnType.Expression {
        return nil, "the returned value is an error rather than a service"
    }

    if "any" == returnType.Expression {
        return nil, "the returned type is any, which cannot identify a service"
    }

    if true == isScalarType(returnType) {
        return nil, "the returned type is a scalar, not a service"
    }

    returnsError := false
    if 2 == returnCount {
        errorType := describeType(results.List[len(results.List)-1].Type, importPath, packageName, fileImports)
        if nil == errorType || "error" != errorType.Expression {
            return nil, "the second returned value is not an error"
        }

        returnsError = true
    }

    arguments := make([]*Argument, 0)

    if nil != functionDeclaration.Type.Params {
        for _, parameter := range functionDeclaration.Type.Params.List {
            if _, isVariadic := parameter.Type.(*ast.Ellipsis); true == isVariadic {
                return nil, "the constructor is variadic"
            }

            if 0 == len(parameter.Names) {
                return nil, "a parameter has no name, so it cannot be bound"
            }

            argumentType := describeType(parameter.Type, importPath, packageName, fileImports)
            if nil == argumentType {
                return nil, "a parameter has a type that cannot be rendered from another package"
            }

            if "error" == argumentType.Expression || "any" == argumentType.Expression {
                return nil, "a parameter is typed " + argumentType.Expression + ", which the container cannot resolve"
            }

            for _, name := range parameter.Names {
                if "_" == name.Name {
                    return nil, "a parameter is blank, so it cannot be bound"
                }

                arguments = append(arguments, &Argument{
                    Name:     name.Name,
                    Type:     argumentType,
                    IsScalar: isScalarType(argumentType),
                })
            }
        }
    }

    if "" != directives.serviceNameIdentifier && false == ast.IsExported(directives.serviceNameIdentifier) {
        return nil, "the service directive names an identifier the generated file cannot reference"
    }

    return &Constructor{
        Name:                  functionDeclaration.Name.Name,
        ImportPath:            importPath,
        PackageName:           packageName,
        ReturnType:            returnType,
        ServiceNameIdentifier: directives.serviceNameIdentifier,
        IsScoped:              directives.isScoped,
        ReturnsError:          returnsError,
        Arguments:             arguments,
        DirectiveBinds:        directives.binds,
    }, ""
}

func describeType(
    expression ast.Expr,
    importPath string,
    packageName string,
    fileImports map[string]string,
) *TypeReference {
    switch typedExpression := expression.(type) {
    case *ast.StarExpr:
        elementType := describeType(typedExpression.X, importPath, packageName, fileImports)
        if nil == elementType {
            return nil
        }

        return &TypeReference{
            Expression: "*" + elementType.Expression,
            Qualifier:  elementType.Qualifier,
            ImportPath: elementType.ImportPath,
            IsPointer:  true,
        }
    case *ast.Ident:
        if true == scalarTypeNames[typedExpression.Name] || "error" == typedExpression.Name || "any" == typedExpression.Name {
            return &TypeReference{
                Expression: typedExpression.Name,
            }
        }

        if false == ast.IsExported(typedExpression.Name) {
            return nil
        }

        if _, hasDotImport := fileImports["."]; true == hasDotImport {
            return nil
        }

        return &TypeReference{
            Expression: packageName + "." + typedExpression.Name,
            Qualifier:  packageName,
            ImportPath: importPath,
        }
    case *ast.SelectorExpr:
        qualifier, isIdentifier := typedExpression.X.(*ast.Ident)
        if false == isIdentifier {
            return nil
        }

        qualifiedImportPath, exists := fileImports[qualifier.Name]
        if false == exists {
            return nil
        }

        return &TypeReference{
            Expression: qualifier.Name + "." + typedExpression.Sel.Name,
            Qualifier:  qualifier.Name,
            ImportPath: qualifiedImportPath,
        }
    default:
        return nil
    }
}

func isScalarType(typeReference *TypeReference) bool {
    if true == typeReference.IsPointer {
        return false
    }

    if true == scalarTypeNames[typeReference.Expression] {
        return true
    }

    return isStandardDuration(typeReference)
}

func isStandardDuration(typeReference *TypeReference) bool {
    if "time" != typeReference.ImportPath {
        return false
    }

    separatorIndex := strings.LastIndex(typeReference.Expression, ".")

    return 0 <= separatorIndex && "Duration" == typeReference.Expression[separatorIndex+1:]
}

func collectImports(fileNode *ast.File) map[string]string {
    fileImports := make(map[string]string)

    for _, importSpec := range fileNode.Imports {
        if nil == importSpec.Name {
            continue
        }

        importPath, unquoteErr := strconv.Unquote(importSpec.Path.Value)
        if nil != unquoteErr {
            continue
        }

        fileImports[importSpec.Name.Name] = importPath
    }

    basePathByName := make(map[string]string)
    baseCountByName := make(map[string]int)

    for _, importSpec := range fileNode.Imports {
        if nil != importSpec.Name {
            continue
        }

        importPath, unquoteErr := strconv.Unquote(importSpec.Path.Value)
        if nil != unquoteErr {
            continue
        }

        base := path.Base(importPath)
        basePathByName[base] = importPath
        baseCountByName[base] = baseCountByName[base] + 1
    }

    for base, count := range baseCountByName {
        if 1 != count {
            continue
        }

        if _, exists := fileImports[base]; false == exists {
            fileImports[base] = basePathByName[base]
        }
    }

    candidatePathByName := make(map[string]string)
    candidateCountByName := make(map[string]int)

    for _, importSpec := range fileNode.Imports {
        if nil != importSpec.Name {
            continue
        }

        importPath, unquoteErr := strconv.Unquote(importSpec.Path.Value)
        if nil != unquoteErr {
            continue
        }

        for _, candidate := range packageNameCandidates(importPath) {

            if 1 < baseCountByName[candidate] {
                continue
            }

            candidatePathByName[candidate] = importPath
            candidateCountByName[candidate] = candidateCountByName[candidate] + 1
        }
    }

    for candidate, count := range candidateCountByName {
        if 1 != count {
            continue
        }

        if _, exists := fileImports[candidate]; false == exists {
            fileImports[candidate] = candidatePathByName[candidate]
        }
    }

    return fileImports
}

func packageNameCandidates(importPath string) []string {
    base := path.Base(importPath)

    candidates := make([]string, 0, 2)

    if true == isMajorVersionSegment(base) {
        parent := path.Base(path.Dir(importPath))
        if "" != parent && "." != parent && "/" != parent {
            candidates = append(candidates, parent)
        }
    }

    if dotIndex := strings.Index(base, "."); 0 < dotIndex {
        candidates = append(candidates, base[:dotIndex])
    }

    if hyphenIndex := strings.LastIndex(base, "-"); 0 <= hyphenIndex && hyphenIndex+1 < len(base) {
        candidates = append(candidates, base[hyphenIndex+1:])
    }

    return candidates
}

func isMajorVersionSegment(value string) bool {
    if 2 > len(value) || 'v' != value[0] {
        return false
    }

    for _, character := range value[1:] {
        if '0' > character || '9' < character {
            return false
        }
    }

    return true
}

func derivedImportPath(rootImportPath string, rootDirectory string, filePath string) (string, error) {
    relativeDirectory, relativeErr := filepath.Rel(rootDirectory, filepath.Dir(filePath))
    if nil != relativeErr {
        return "", exception.NewError(
            "could not derive the import path of a scanned file",
            map[string]any{
                "file": filePath,
            },
            relativeErr,
        )
    }

    if "." == relativeDirectory {
        return rootImportPath, nil
    }

    return rootImportPath + "/" + filepath.ToSlash(relativeDirectory), nil
}

func isExcluded(typeExpression string, excludes []string, matchedExcludes map[string]bool) bool {
    typeName := strings.TrimPrefix(typeExpression, "*")

    if separatorIndex := strings.Index(typeName, "."); 0 <= separatorIndex {
        typeName = typeName[separatorIndex+1:]
    }

    excluded := false
    for _, pattern := range excludes {
        matched, matchErr := path.Match(pattern, typeName)
        if nil != matchErr {
            continue
        }

        if true == matched {
            matchedExcludes[pattern] = true
            excluded = true
        }
    }

    return excluded
}
