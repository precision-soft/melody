package main

import (
    "bufio"
    "flag"
    "fmt"
    "go/ast"
    "go/constant"
    "go/token"
    "go/types"
    "os"
    "path/filepath"
    "regexp"
    "sort"
    "strings"
    "time"

    "golang.org/x/tools/go/packages"
)

const (
    boundPackagePath  = "github.com/precision-soft/melody/v4/bound"
    configPackagePath = "github.com/precision-soft/melody/v4/config"
    internalPath      = "github.com/precision-soft/melody/v4/internal"

    kindLimit      = "limit"
    kindProtection = "protection"
    kindRefusal    = "refusal"
    kindRedaction  = "redaction"
    kindConstant   = "constant"
    kindMode       = "mode"
    kindWarning    = "warning"
    kindSwitch     = "switch"

    /* the failure kinds that are not row kinds */
    kindRow    = "row"
    kindAnchor = "anchor"
    kindDoor   = "door"

    doorOff   = "off"
    doorValue = "value"
    doorUse   = "use"
    doorEnv   = "env"
)

var rowKindSet = map[string]bool{
    kindLimit:      true,
    kindProtection: true,
    kindRefusal:    true,
    kindRedaction:  true,
    kindConstant:   true,
    kindMode:       true,
    kindWarning:    true,
    kindSwitch:     true,
}

/* the kinds an item enumerated from the code carries; a row of another kind is a hand row, kept alive by its anchor */
var enumeratedKindSet = map[string]bool{
    kindLimit:   true,
    kindMode:    true,
    kindWarning: true,
}

/* an env: door turns nothing off on these kinds: it sets an operational value */
var environmentDoorKindSet = map[string]bool{
    kindLimit:    true,
    kindMode:     true,
    kindWarning:  true,
    kindConstant: true,
    kindSwitch:   true,
}

var modeFunctionSet = map[string]int{
    "MkdirAll":  1,
    "Mkdir":     1,
    "OpenFile":  2,
    "WriteFile": 2,
    "Chmod":     1,
}

var bareClassReferencePattern = regexp.MustCompile(`^class [A-Z]$`)

/* a limit's default cell states a figure, unlimited, or that the value has no default */
var limitDefaultFigurePattern = regexp.MustCompile(`[0-9]|unlimited|no default`)

/* a field so named is a door whatever its type: a list of what a route turns off is as much a door as a bool */
var doorFieldNamePattern = regexp.MustCompile(`Disabled$`)

const melodyModulePathPrefix = "github.com/precision-soft/melody"

/* a door field is a field of a configuration: a struct named like one; a result such as a decision or a payload carries no door */
var configurationTypeNamePattern = regexp.MustCompile(`(Config|Configuration|Options|Policy|Override|Definition|Binding|Ring)$`)

/* item is one default the code declares: a rule, a boot-warning code or a mode site */
type item struct {
    kind              string
    key               string
    position          string
    unlimitedAdmitted bool
    hasMaximum        bool
    /* the rule's default and bounds as the code spells them, for a draft row */
    boundText string
}

/* door is one anchor of a row's door cell with its class */
type door struct {
    class  string
    anchor string
}

/* row is one data line of the document */
type row struct {
    key        string
    kind       string
    protects   string
    defaultCell string
    doorList   []door
    doorNone   bool
    reason     string
    lineNumber int
}

/* failure is one disagreement between the code and the document, keyed for the baseline */
type failure struct {
    kind    string
    key     string
    message string
}

/* baselineLine is one held failure */
type baselineLine struct {
    kind       string
    key        string
    reason     string
    lineNumber int
}

/* inventory is what the code of every v4-line module declares */
type inventory struct {
    itemMap         map[string]item
    packageMap      map[string]*types.Package
    /* the repository path of a package, by its import path */
    repositoryPathMap map[string]string
    environmentMap  map[string]string
    doorFieldMap    map[string]string
    /* the first sentence of a door field's comment, for a draft row */
    doorFieldTextMap map[string]string
    packageErrorList []string
}

/* notDoorLine is one field of a configuration the band would read as a door, written off with the reason it turns nothing off */
type notDoorLine struct {
    field      string
    reason     string
    lineNumber int
}

func main() {
    rootFlag := flag.String("root", ".", "the repository root the module directories are relative to")
    documentFlag := flag.String("document", "v4/.documentation/DEFAULTS.md", "the inventory document, relative to the root")
    baselineFlag := flag.String("baseline", ".dev/validate/defaults.baseline", "the baseline, relative to the root")
    notDoorFlag := flag.String("not-doors", ".dev/validate/defaults.not-doors", "the fields that are no door, with their reason, relative to the root")
    draftFlag := flag.Bool("draft", false, "print a draft row for every enumerated item the document has no row for, instead of checking")
    flag.Parse()

    moduleList := flag.Args()
    if 0 == len(moduleList) {
        fmt.Fprintln(os.Stderr, "name the v4-line module directories, relative to the root")
        os.Exit(2)
    }

    measured, measureErr := measure(*rootFlag, moduleList, os.Environ())
    if nil != measureErr {
        fmt.Fprintf(os.Stderr, "%v\n", measureErr)
        os.Exit(2)
    }

    rowList, parseFailureList, readErr := readDocument(filepath.Join(*rootFlag, *documentFlag))
    if nil != readErr {
        fmt.Fprintf(os.Stderr, "%v\n", readErr)
        os.Exit(2)
    }

    notDoorList, notDoorErr := readNotDoors(filepath.Join(*rootFlag, *notDoorFlag))
    if nil != notDoorErr {
        fmt.Fprintf(os.Stderr, "%v\n", notDoorErr)
        os.Exit(2)
    }

    if true == *draftFlag {
        printDraft(measured, rowList, notDoorList)

        return
    }

    baselineList, baselineErr := readBaseline(filepath.Join(*rootFlag, *baselineFlag))
    if nil != baselineErr {
        fmt.Fprintf(os.Stderr, "%v\n", baselineErr)
        os.Exit(2)
    }

    failureList := append(parseFailureList, check(measured, rowList, notDoorList)...)
    openList, staleList := applyBaseline(failureList, baselineList)

    for _, open := range openList {
        fmt.Printf("%s ~ %s ~ %s\n", open.kind, open.key, open.message)
    }
    for _, stale := range staleList {
        fmt.Printf("baseline:%d %s ~ %s matches no failure: delete the line\n", stale.lineNumber, stale.kind, stale.key)
    }

    fmt.Fprintf(os.Stderr, "defaults: %d item(s), %d row(s), %d door field(s), %d held by the baseline, %d open, %d stale baseline line(s)\n", len(measured.itemMap), len(rowList), len(measured.doorFieldMap), len(failureList)-len(openList), len(openList), len(staleList))

    if 0 < len(openList) || 0 < len(staleList) {
        os.Exit(1)
    }
}

/* measure loads every package of each module and enumerates its items, its env keys, its door fields and the packages an anchor resolves in. A package that does not type-check fails the run: an inventory read off a broken package is not whole. */
func measure(root string, moduleList []string, environment []string) (*inventory, error) {
    absoluteRoot, absoluteErr := filepath.Abs(root)
    if nil != absoluteErr {
        return nil, fmt.Errorf("resolve the root %s: %w", root, absoluteErr)
    }

    measured := &inventory{
        itemMap:        map[string]item{},
        packageMap:     map[string]*types.Package{},
        repositoryPathMap: map[string]string{},
        environmentMap: map[string]string{},
        doorFieldMap:   map[string]string{},
        doorFieldTextMap: map[string]string{},
    }

    for _, moduleDirectory := range moduleList {
        if loadErr := measureModule(absoluteRoot, filepath.ToSlash(filepath.Clean(moduleDirectory)), environment, measured); nil != loadErr {
            return nil, loadErr
        }
    }

    if 0 < len(measured.packageErrorList) {
        return nil, fmt.Errorf("a module does not type-check, so its defaults cannot be read whole:\n%s", strings.Join(measured.packageErrorList, "\n"))
    }

    return measured, nil
}

func measureModule(absoluteRoot string, moduleDirectory string, environment []string, measured *inventory) error {
    absoluteModuleDirectory := filepath.Join(absoluteRoot, moduleDirectory)

    configuration := &packages.Config{
        Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule,
        Dir:  absoluteModuleDirectory,
        Env:  environment,
    }

    loadedPackageList, loadErr := packages.Load(configuration, "./...")
    if nil != loadErr {
        return fmt.Errorf("load the packages of %s: %w", moduleDirectory, loadErr)
    }

    for _, loadedPackage := range loadedPackageList {
        if nil == loadedPackage.Module || absoluteModuleDirectory != filepath.Clean(loadedPackage.Module.Dir) {
            continue
        }

        for _, packageError := range loadedPackage.Errors {
            measured.packageErrorList = append(measured.packageErrorList, fmt.Sprintf("%s: %s", loadedPackage.PkgPath, packageError.Error()))
        }
        if nil == loadedPackage.Types || nil == loadedPackage.TypesInfo {
            continue
        }

        packagePath := moduleDirectory
        relativePackagePath := strings.TrimPrefix(strings.TrimPrefix(loadedPackage.PkgPath, loadedPackage.Module.Path), "/")
        if "" != relativePackagePath {
            packagePath = moduleDirectory + "/" + relativePackagePath
        }

        measured.packageMap[packagePath] = loadedPackage.Types
        measured.repositoryPathMap[loadedPackage.PkgPath] = packagePath

        if boundPackagePath == loadedPackage.PkgPath {
            continue
        }

        measurePackage(absoluteRoot, packagePath, loadedPackage, measured)
    }

    return nil
}

func measurePackage(absoluteRoot string, packagePath string, loadedPackage *packages.Package, measured *inventory) {
    information := loadedPackage.TypesInfo
    positionOf := func(position token.Pos) string {
        resolved := loadedPackage.Fset.Position(position)
        relativeFile, relativeErr := filepath.Rel(absoluteRoot, resolved.Filename)
        if nil != relativeErr {
            relativeFile = resolved.Filename
        }

        return fmt.Sprintf("%s:%d", filepath.ToSlash(relativeFile), resolved.Line)
    }

    measureConstants(packagePath, loadedPackage, measured, positionOf)

    commentMap := map[token.Pos]string{}
    for _, file := range loadedPackage.Syntax {
        ast.Inspect(file, func(node ast.Node) bool {
            if field, isField := node.(*ast.Field); true == isField && 0 < len(field.Names) {
                text := field.Doc.Text() + field.Comment.Text()
                commentMap[field.Names[0].Pos()] = strings.Join(strings.Fields(text), " ")
            }

            return true
        })
    }
    measureDoorFields(packagePath, loadedPackage.Types, measured, positionOf, commentMap)

    siteCount := map[string]int{}
    for _, file := range loadedPackage.Syntax {
        for _, declaration := range file.Decls {
            ownerName := declarationOwner(declaration)

            ast.Inspect(declaration, func(node ast.Node) bool {
                switch typed := node.(type) {
                case *ast.CompositeLit:
                    measureRuleLiteral(packagePath, ownerName, typed, information, measured, siteCount, positionOf)
                    measureEnvironmentKeyLiteral(typed, information, measured, positionOf)
                case *ast.CallExpr:
                    measureCall(packagePath, ownerName, typed, information, measured, siteCount, positionOf)
                }

                return true
            })
        }
    }
}

/* declarationOwner names the declaration a site is counted under: a function, a method as Type.Method, or the first name of a var block */
func declarationOwner(declaration ast.Decl) string {
    switch typed := declaration.(type) {
    case *ast.FuncDecl:
        if nil != typed.Recv && 0 < len(typed.Recv.List) {
            return receiverTypeName(typed.Recv.List[0].Type) + "." + typed.Name.Name
        }

        return typed.Name.Name
    case *ast.GenDecl:
        for _, specification := range typed.Specs {
            if valueSpecification, isValue := specification.(*ast.ValueSpec); true == isValue && 0 < len(valueSpecification.Names) {
                return valueSpecification.Names[0].Name
            }
        }
    }

    return "package"
}

func receiverTypeName(expression ast.Expr) string {
    switch typed := expression.(type) {
    case *ast.StarExpr:
        return receiverTypeName(typed.X)
    case *ast.IndexExpr:
        return receiverTypeName(typed.X)
    case *ast.IndexListExpr:
        return receiverTypeName(typed.X)
    case *ast.Ident:
        return typed.Name
    }

    return "receiver"
}

/* isBoundType answers whether a type is the named type of the bound package given by name, an instantiation included */
func isBoundType(candidate types.Type, name string) bool {
    if nil == candidate {
        return false
    }

    if pointer, isPointer := candidate.(*types.Pointer); true == isPointer {
        candidate = pointer.Elem()
    }

    named, isNamed := types.Unalias(candidate).(*types.Named)
    if false == isNamed || nil == named.Obj().Pkg() {
        return false
    }

    return boundPackagePath == named.Obj().Pkg().Path() && name == named.Obj().Name()
}

func measureRuleLiteral(packagePath string, ownerName string, literal *ast.CompositeLit, information *types.Info, measured *inventory, siteCount map[string]int, positionOf func(token.Pos) string) {
    literalType := information.TypeOf(literal)
    if false == isBoundType(literalType, "Rule") && false == isBoundType(literalType, "BackoffRule") {
        return
    }

    measuredItem := item{kind: kindLimit, position: positionOf(literal.Pos())}
    ruleName := ""
    for _, element := range literal.Elts {
        keyValue, isKeyValue := element.(*ast.KeyValueExpr)
        if false == isKeyValue {
            continue
        }

        fieldIdentifier, isIdentifier := keyValue.Key.(*ast.Ident)
        if false == isIdentifier {
            continue
        }

        value := information.Types[keyValue.Value].Value
        switch fieldIdentifier.Name {
        case "Name":
            if nil != value && constant.String == value.Kind() {
                ruleName = constant.StringVal(value)
            }
        case "UnlimitedAdmitted", "DefaultUnlimited":
            if nil == value || constant.Bool != value.Kind() || true == constant.BoolVal(value) {
                measuredItem.unlimitedAdmitted = true
            }
        case "Maximum":
            measuredItem.hasMaximum = true
        }
        switch fieldIdentifier.Name {
        case "Default", "Minimum", "Maximum", "InitialDefault", "MaxDefault":
            measuredItem.boundText += fieldIdentifier.Name + "=" + types.ExprString(keyValue.Value) + figureOf(value, information.TypeOf(keyValue.Value)) + " "
        case "DefaultUnlimited", "UnlimitedAdmitted":
            measuredItem.boundText += fieldIdentifier.Name + " "
        }
    }

    if "" == ruleName {
        siteCount[ownerName]++
        ruleName = fmt.Sprintf("%s#%d", ownerName, siteCount[ownerName])
    }

    measuredItem.key = packagePath + "." + ruleName
    measured.itemMap[measuredItem.key] = measuredItem
}

func measureEnvironmentKeyLiteral(literal *ast.CompositeLit, information *types.Info, measured *inventory, positionOf func(token.Pos) string) {
    named, isNamed := types.Unalias(information.TypeOf(literal)).(*types.Named)
    if false == isNamed || nil == named.Obj().Pkg() || configPackagePath+"/contract" != named.Obj().Pkg().Path() || "EnvironmentKey" != named.Obj().Name() {
        return
    }

    for _, element := range literal.Elts {
        keyValue, isKeyValue := element.(*ast.KeyValueExpr)
        if false == isKeyValue {
            continue
        }

        if fieldIdentifier, isIdentifier := keyValue.Key.(*ast.Ident); false == isIdentifier || "Key" != fieldIdentifier.Name {
            continue
        }

        if value := information.Types[keyValue.Value].Value; nil != value && constant.String == value.Kind() {
            measured.environmentMap[constant.StringVal(value)] = positionOf(literal.Pos())
        }
    }
}

func measureCall(packagePath string, ownerName string, call *ast.CallExpr, information *types.Info, measured *inventory, siteCount map[string]int, positionOf func(token.Pos) string) {
    selector, isSelector := call.Fun.(*ast.SelectorExpr)
    if false == isSelector {
        return
    }

    function, isFunction := information.Uses[selector.Sel].(*types.Func)
    if false == isFunction {
        return
    }

    if "DeclareBootWarning" == function.Name() && 0 < len(call.Args) {
        if value := information.Types[call.Args[0]].Value; nil != value && constant.String == value.Kind() {
            key := constant.StringVal(value)
            measured.itemMap[key] = item{kind: kindWarning, key: key, position: positionOf(call.Pos())}
        }

        return
    }

    modeArgumentIndex, isModeFunction := modeFunctionSet[function.Name()]
    if false == isModeFunction || nil == function.Pkg() || "os" != function.Pkg().Path() || len(call.Args) <= modeArgumentIndex {
        return
    }

    signature, isSignature := function.Type().(*types.Signature)
    if false == isSignature {
        return
    }
    if nil != signature.Recv() && false == isOsRoot(signature.Recv().Type()) {
        return
    }

    if value := information.Types[call.Args[modeArgumentIndex]].Value; nil == value {
        return
    }

    siteCount["mode "+ownerName]++
    key := fmt.Sprintf("%s.%s#%d", packagePath, ownerName, siteCount["mode "+ownerName])
    measured.itemMap[key] = item{kind: kindMode, key: key, position: positionOf(call.Pos()), boundText: function.Name() + " " + types.ExprString(call.Args[modeArgumentIndex])}
}

/* figureOf renders a constant bound as the figure a row's default cell states, a duration in its own unit; a bound the code computes renders nothing */
func figureOf(value constant.Value, valueType types.Type) string {
    if nil == value {
        return ""
    }

    if named, isNamed := types.Unalias(valueType).(*types.Named); true == isNamed && nil != named.Obj().Pkg() && "time" == named.Obj().Pkg().Path() && "Duration" == named.Obj().Name() {
        if nanoseconds, isExact := constant.Int64Val(value); true == isExact {
            return "(" + time.Duration(nanoseconds).String() + ")"
        }
    }

    return "(" + value.ExactString() + ")"
}

/* isFileMode answers whether a type is fs.FileMode, which os.FileMode aliases */
func isFileMode(candidate types.Type) bool {
    named, isNamed := types.Unalias(candidate).(*types.Named)

    return true == isNamed && nil != named.Obj().Pkg() && "io/fs" == named.Obj().Pkg().Path() && "FileMode" == named.Obj().Name()
}

func isOsRoot(candidate types.Type) bool {
    if pointer, isPointer := candidate.(*types.Pointer); true == isPointer {
        candidate = pointer.Elem()
    }

    named, isNamed := candidate.(*types.Named)

    return true == isNamed && nil != named.Obj().Pkg() && "os" == named.Obj().Pkg().Path() && "Root" == named.Obj().Name()
}

/* measureConstants reads the framework's boot-warning codes and its env keys off their declarations */
func measureConstants(packagePath string, loadedPackage *packages.Package, measured *inventory, positionOf func(token.Pos) string) {
    scope := loadedPackage.Types.Scope()
    for _, objectName := range scope.Names() {
        constantObject, isConstant := scope.Lookup(objectName).(*types.Const)
        if false == isConstant {
            continue
        }

        if true == isFileMode(constantObject.Type()) {
            key := packagePath + "." + objectName
            measured.itemMap[key] = item{kind: kindMode, key: key, position: positionOf(constantObject.Pos()), boundText: fmt.Sprintf("%#o", constantObject.Val())}

            continue
        }

        if constant.String != constantObject.Val().Kind() {
            continue
        }

        value := constant.StringVal(constantObject.Val())
        if internalPath == loadedPackage.PkgPath && true == strings.HasPrefix(objectName, "BootWarningName") {
            measured.itemMap[value] = item{kind: kindWarning, key: value, position: positionOf(constantObject.Pos())}
        }

        if configPackagePath == loadedPackage.PkgPath && true == strings.HasSuffix(objectName, "Key") && true == strings.HasPrefix(value, "MELODY_") {
            measured.environmentMap[value] = positionOf(constantObject.Pos())
        }
    }
}

/* measureDoorFields lists every exported field of an exported configuration struct that may be a door: every bool, every mode typed by an enumeration of the line's own, every *Disabled field whatever its type, every bound limit. A field that turns nothing off is written off in the not-doors file with its reason. */
func measureDoorFields(packagePath string, packageTypes *types.Package, measured *inventory, positionOf func(token.Pos) string, commentMap map[token.Pos]string) {
    scope := packageTypes.Scope()
    for _, objectName := range scope.Names() {
        typeName, isTypeName := scope.Lookup(objectName).(*types.TypeName)
        if false == isTypeName || false == typeName.Exported() || true == typeName.IsAlias() || false == configurationTypeNamePattern.MatchString(objectName) {
            continue
        }

        structType, isStruct := typeName.Type().Underlying().(*types.Struct)
        if false == isStruct {
            continue
        }

        for fieldIndex := 0; fieldIndex < structType.NumFields(); fieldIndex++ {
            field := structType.Field(fieldIndex)
            if false == field.Exported() || true == field.Embedded() {
                continue
            }

            isDoor := isBoundType(field.Type(), "Limit") || isBoundType(field.Type(), "Backoff") || true == doorFieldNamePattern.MatchString(field.Name())
            if basic, isBasic := field.Type().Underlying().(*types.Basic); true == isBasic && types.Bool == basic.Kind() {
                isDoor = true
            }
            if true == isEnumeration(field.Type()) {
                isDoor = true
            }

            if true == isDoor {
                measured.doorFieldMap[packagePath+"."+objectName+"."+field.Name()] = positionOf(field.Pos())
                measured.doorFieldTextMap[packagePath+"."+objectName+"."+field.Name()] = commentMap[field.Pos()]
            }
        }
    }
}

/* isEnumeration answers whether a type is a named string or integer type of the line whose package declares constants of it: a mode, whose values are the doors */
func isEnumeration(candidate types.Type) bool {
    named, isNamed := types.Unalias(candidate).(*types.Named)
    if false == isNamed || nil == named.Obj().Pkg() || false == strings.HasPrefix(named.Obj().Pkg().Path(), melodyModulePathPrefix) {
        return false
    }

    basic, isBasic := named.Underlying().(*types.Basic)
    if false == isBasic || 0 == basic.Info()&(types.IsString|types.IsInteger) {
        return false
    }

    scope := named.Obj().Pkg().Scope()
    for _, objectName := range scope.Names() {
        if constantObject, isConstant := scope.Lookup(objectName).(*types.Const); true == isConstant && true == constantObject.Exported() && true == types.Identical(constantObject.Type(), named) {
            return true
        }
    }

    return false
}

/* resolveAnchor answers whether `<package path>.<Symbol>[.<Field or Method>…]` names a declaration; the package path is everything before the first dot after its last slash */
func (instance *inventory) resolveAnchor(anchor string) bool {
    _, isResolved := instance.canonicalAnchor(anchor)

    return isResolved
}

/* isReader answers whether an anchor names a function or a method that takes no argument */
func (instance *inventory) isReader(anchor string) bool {
    lastSlash := strings.LastIndex(anchor, "/")
    firstDot := strings.Index(anchor[lastSlash+1:], ".")
    if -1 == firstDot {
        return false
    }

    packageTypes, isPackage := instance.packageMap[anchor[:lastSlash+1+firstDot]]
    if false == isPackage {
        return false
    }

    segmentList := strings.Split(anchor[lastSlash+1+firstDot+1:], ".")
    object := packageTypes.Scope().Lookup(segmentList[0])
    for _, segment := range segmentList[1:] {
        if nil == object {
            return false
        }
        object, _, _ = types.LookupFieldOrMethod(object.Type(), true, packageTypes, segment)
    }

    function, isFunction := object.(*types.Func)
    if false == isFunction {
        return false
    }

    signature, isSignature := function.Type().(*types.Signature)

    return true == isSignature && 0 == signature.Params().Len()
}

/* canonicalAnchor resolves an anchor and answers the declaration it names as `<package path>.<Type>.<Field>` when it ends on a field reached through other types, so a door cited through a nested path counts for the field's own struct */
func (instance *inventory) canonicalAnchor(anchor string) (string, bool) {
    lastSlash := strings.LastIndex(anchor, "/")
    firstDot := strings.Index(anchor[lastSlash+1:], ".")
    if -1 == firstDot {
        _, isPackage := instance.packageMap[anchor]

        return anchor, isPackage
    }

    packagePath := anchor[:lastSlash+1+firstDot]
    packageTypes, isPackage := instance.packageMap[packagePath]
    if false == isPackage {
        return "", false
    }

    segmentList := strings.Split(anchor[lastSlash+1+firstDot+1:], ".")
    object := packageTypes.Scope().Lookup(segmentList[0])
    if nil == object {
        return "", false
    }

    canonical := anchor
    for _, segment := range segmentList[1:] {
        found, _, _ := types.LookupFieldOrMethod(object.Type(), true, packageTypes, segment)
        if nil == found {
            return "", false
        }

        canonical = anchor
        ownerType := object.Type()
        if pointer, isPointer := ownerType.(*types.Pointer); true == isPointer {
            ownerType = pointer.Elem()
        }
        if named, isNamed := types.Unalias(ownerType).(*types.Named); true == isNamed && nil != named.Obj().Pkg() {
            if ownerPath, isKnown := instance.repositoryPathMap[named.Obj().Pkg().Path()]; true == isKnown {
                canonical = ownerPath + "." + named.Obj().Name() + "." + segment
            }
        }
        object = found
    }

    return canonical, true
}

/* readDocument reads every data row of the inventory's tables: a table is one whose header is the seven columns */
func readDocument(documentPath string) ([]row, []failure, error) {
    file, openErr := os.Open(documentPath)
    if nil != openErr {
        return nil, nil, fmt.Errorf("open the inventory %s: %w", documentPath, openErr)
    }
    defer file.Close()

    rowList := []row{}
    failureList := []failure{}
    inTable := false
    lineNumber := 0
    scanner := bufio.NewScanner(file)
    scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
    for scanner.Scan() {
        lineNumber++
        line := strings.TrimSpace(scanner.Text())
        if false == strings.HasPrefix(line, "|") {
            inTable = false

            continue
        }

        cellList := splitRow(line)
        if 7 == len(cellList) && "key" == cellList[0] && "kind" == cellList[1] {
            inTable = true

            continue
        }
        if false == inTable || true == strings.HasPrefix(cellList[0], "---") {
            continue
        }

        if 7 != len(cellList) {
            failureList = append(failureList, failure{kind: kindRow, key: fmt.Sprintf("line %d", lineNumber), message: fmt.Sprintf("the row has %d cells, the table has 7", len(cellList))})

            continue
        }

        parsed := row{
            key:        strings.Trim(cellList[0], "`"),
            kind:       cellList[1],
            protects:   cellList[2],
            defaultCell: cellList[3],
            reason:     cellList[5],
            lineNumber: lineNumber,
        }
        if "—" == parsed.reason {
            parsed.reason = ""
        }

        if "none" == cellList[4] {
            parsed.doorNone = true
        } else {
            for _, span := range backtickSpanList(cellList[4]) {
                separator := strings.Index(span, ":")
                if -1 == separator {
                    parsed.doorList = append(parsed.doorList, door{anchor: span})

                    continue
                }
                parsed.doorList = append(parsed.doorList, door{class: span[:separator], anchor: span[separator+1:]})
            }
        }

        rowList = append(rowList, parsed)
    }

    if scanErr := scanner.Err(); nil != scanErr {
        return nil, nil, fmt.Errorf("read the inventory %s: %w", documentPath, scanErr)
    }

    return rowList, failureList, nil
}

func splitRow(line string) []string {
    trimmed := strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
    cellList := strings.Split(trimmed, "|")
    for index := range cellList {
        cellList[index] = strings.TrimSpace(cellList[index])
    }

    return cellList
}

func backtickSpanList(cell string) []string {
    spanList := []string{}
    partList := strings.Split(cell, "`")
    for index := 1; index < len(partList); index += 2 {
        if "" != strings.TrimSpace(partList[index]) {
            spanList = append(spanList, strings.TrimSpace(partList[index]))
        }
    }

    return spanList
}

/* check runs the band's checks: every item has its row, every row its columns, every anchor its declaration, every door field its row */
func check(measured *inventory, rowList []row, notDoorList []notDoorLine) []failure {
    failureList := []failure{}
    seenLine := map[string]int{}
    citedDoor := map[string]bool{}

    for _, current := range rowList {
        if firstLine, isSeen := seenLine[current.key]; true == isSeen {
            failureList = append(failureList, failure{kind: kindRow, key: current.key, message: fmt.Sprintf("line %d repeats the key of line %d", current.lineNumber, firstLine)})

            continue
        }
        seenLine[current.key] = current.lineNumber

        failureList = append(failureList, checkRow(measured, current, citedDoor)...)
    }

    for _, key := range sortedKeyList(measured.itemMap) {
        if _, hasRow := seenLine[key]; false == hasRow {
            measuredItem := measured.itemMap[key]
            failureList = append(failureList, failure{kind: measuredItem.kind, key: key, message: fmt.Sprintf("declared at %s, has no row", measuredItem.position)})
        }
    }

    notDoorSet := map[string]bool{}
    for _, line := range notDoorList {
        if _, isField := measured.doorFieldMap[line.field]; false == isField {
            failureList = append(failureList, failure{kind: kindDoor, key: line.field, message: fmt.Sprintf("not-doors:%d names no field the band reads as a door: delete the line", line.lineNumber)})

            continue
        }
        if true == citedDoor[line.field] {
            failureList = append(failureList, failure{kind: kindDoor, key: line.field, message: fmt.Sprintf("not-doors:%d writes off a field a row names as a door: one of the two is wrong", line.lineNumber)})
        }
        notDoorSet[line.field] = true
    }

    for _, field := range sortedKeyList(measured.doorFieldMap) {
        if false == citedDoor[field] && false == notDoorSet[field] {
            failureList = append(failureList, failure{kind: kindDoor, key: field, message: fmt.Sprintf("declared at %s, no row names it as a door and no not-doors line says why it is none", measured.doorFieldMap[field])})
        }
    }

    return failureList
}

func checkRow(measured *inventory, current row, citedDoor map[string]bool) []failure {
    failureList := []failure{}
    fail := func(kind string, message string) {
        failureList = append(failureList, failure{kind: kind, key: current.key, message: fmt.Sprintf("line %d: %s", current.lineNumber, message)})
    }

    if false == rowKindSet[current.kind] {
        fail(kindRow, fmt.Sprintf("the kind %q is none of limit, protection, refusal, redaction, constant, mode, warning, switch", current.kind))

        return failureList
    }

    measuredItem, isItem := measured.itemMap[current.key]
    if true == enumeratedKindSet[current.kind] {
        if false == isItem || current.kind != measuredItem.kind {
            fail(current.kind, "the code declares no such "+current.kind+": delete the row")
        }
    } else if false == measured.resolveAnchor(current.key) {
        fail(kindAnchor, "the key names no declaration")
    }

    if false == current.doorNone && 0 == len(current.doorList) {
        fail(kindRow, "the door is empty: name an anchor or write none")
    }
    if true == strings.HasPrefix(current.protects, "the door:") || current.protects == current.key[strings.LastIndex(current.key, ".")+1:] {
        fail(kindRow, "the protects-against cell is a draft: write what goes wrong without the default")
    }
    if true == current.doorNone && "" == current.reason {
        fail(kindRow, "the door is none and the reason is empty")
    }

    hasOffOrValue := false
    hasValue := false
    for _, rowDoor := range current.doorList {
        switch rowDoor.class {
        case doorOff, doorValue, doorUse:
            canonical, isResolved := measured.canonicalAnchor(rowDoor.anchor)
            if false == isResolved {
                fail(kindAnchor, fmt.Sprintf("the door %s:%s names no declaration", rowDoor.class, rowDoor.anchor))
            } else if doorValue == rowDoor.class && true == measured.isReader(rowDoor.anchor) {
                fail(kindAnchor, fmt.Sprintf("the door value:%s is a function that takes nothing: it answers a value and sets none, name what takes it", rowDoor.anchor))
            }
            citedDoor[rowDoor.anchor] = true
            citedDoor[canonical] = true
            hasOffOrValue = hasOffOrValue || doorUse != rowDoor.class
            hasValue = hasValue || doorValue == rowDoor.class
        case doorEnv:
            if _, isKey := measured.environmentMap[rowDoor.anchor]; false == isKey {
                fail(kindAnchor, fmt.Sprintf("the door env:%s names no declared env key", rowDoor.anchor))
            }
            if kindProtection == current.kind && false == strings.Contains(current.reason, "hardens") {
                fail(kindRow, fmt.Sprintf("the door env:%s turns a protection off from the environment: a protection is turned off in Go only, an env key may only harden it", rowDoor.anchor))
            } else if kindProtection != current.kind && false == environmentDoorKindSet[current.kind] {
                fail(kindRow, fmt.Sprintf("the door env:%s on a %s row: an env key sets an operational value only", rowDoor.anchor, current.kind))
            }
        default:
            fail(kindRow, fmt.Sprintf("the door %q has no class off:, value:, use: or env:", rowDoor.anchor))
        }
    }

    if kindLimit == current.kind && false == limitDefaultFigurePattern.MatchString(current.defaultCell) {
        fail(kindLimit, "the default cell states no figure: write the value that applies when nothing is configured")
    }

    if kindLimit == current.kind && true == isItem {
        if true == measuredItem.unlimitedAdmitted {
            if false == hasOffOrValue {
                fail(kindLimit, "the rule admits unlimited: its door is off: or value:")
            }
        } else {
            if false == hasValue {
                fail(kindLimit, "the rule refuses unlimited: its door is value:")
            }
            if "" == current.reason {
                fail(kindLimit, "the rule refuses unlimited and the reason is empty")
            } else if true == measuredItem.hasMaximum && true == bareClassReferencePattern.MatchString(current.reason) {
                fail(kindLimit, "the rule clamps at a maximum: the reason is its own, not a bare class reference")
            }
        }
    }

    return failureList
}

/* readNotDoors reads `field ~ reason` lines; a line without its reason, or a field written off twice, is refused */
func readNotDoors(notDoorPath string) ([]notDoorLine, error) {
    file, openErr := os.Open(notDoorPath)
    if nil != openErr {
        return nil, fmt.Errorf("open the not-doors file %s: %w", notDoorPath, openErr)
    }
    defer file.Close()

    lineList := []notDoorLine{}
    seenLine := map[string]int{}
    lineNumber := 0
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        lineNumber++
        line := strings.TrimSpace(scanner.Text())
        if "" == line || true == strings.HasPrefix(line, "#") {
            continue
        }

        partList := strings.SplitN(line, "~", 2)
        if 2 != len(partList) || "" == strings.TrimSpace(partList[0]) || "" == strings.TrimSpace(partList[1]) {
            return nil, fmt.Errorf("%s:%d: a not-doors line is field ~ reason, the reason written", notDoorPath, lineNumber)
        }

        field := strings.TrimSpace(partList[0])
        if firstLine, isSeen := seenLine[field]; true == isSeen {
            return nil, fmt.Errorf("%s:%d: %s repeats line %d", notDoorPath, lineNumber, field, firstLine)
        }
        seenLine[field] = lineNumber

        lineList = append(lineList, notDoorLine{field: field, reason: strings.TrimSpace(partList[1]), lineNumber: lineNumber})
    }

    return lineList, scanner.Err()
}

/* readBaseline reads `kind ~ key ~ reason` lines; a line without its reason is refused */
func readBaseline(baselinePath string) ([]baselineLine, error) {
    file, openErr := os.Open(baselinePath)
    if nil != openErr {
        return nil, fmt.Errorf("open the baseline %s: %w", baselinePath, openErr)
    }
    defer file.Close()

    lineList := []baselineLine{}
    lineNumber := 0
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        lineNumber++
        line := strings.TrimSpace(scanner.Text())
        if "" == line || true == strings.HasPrefix(line, "#") {
            continue
        }

        partList := strings.SplitN(line, "~", 3)
        if 3 != len(partList) || "" == strings.TrimSpace(partList[2]) {
            return nil, fmt.Errorf("%s:%d: a baseline line is kind ~ key ~ reason, the reason written", baselinePath, lineNumber)
        }

        lineList = append(lineList, baselineLine{
            kind:       strings.TrimSpace(partList[0]),
            key:        strings.TrimSpace(partList[1]),
            reason:     strings.TrimSpace(partList[2]),
            lineNumber: lineNumber,
        })
    }

    return lineList, scanner.Err()
}

/* applyBaseline holds every failure a line names by kind and key, and answers the open failures and the lines that hold nothing */
func applyBaseline(failureList []failure, baselineList []baselineLine) ([]failure, []baselineLine) {
    heldSet := map[string]bool{}
    for _, line := range baselineList {
        heldSet[line.kind+"\t"+line.key] = true
    }

    matchedSet := map[string]bool{}
    openList := []failure{}
    for _, current := range failureList {
        identity := current.kind + "\t" + current.key
        if true == heldSet[identity] {
            matchedSet[identity] = true

            continue
        }
        openList = append(openList, current)
    }

    staleList := []baselineLine{}
    for _, line := range baselineList {
        if false == matchedSet[line.kind+"\t"+line.key] {
            staleList = append(staleList, line)
        }
    }

    sort.SliceStable(openList, func(left int, right int) bool {
        if openList[left].kind != openList[right].kind {
            return openList[left].kind < openList[right].kind
        }

        return openList[left].key < openList[right].key
    })

    return openList, staleList
}

/* printDraft prints a draft row for every enumerated item and door field the document does not name, grouped by package, for the hand that writes the rows */
func printDraft(measured *inventory, rowList []row, notDoorList []notDoorLine) {
    namedSet := map[string]bool{}
    for _, line := range notDoorList {
        namedSet[line.field] = true
    }
    for _, current := range rowList {
        namedSet[current.key] = true
        for _, rowDoor := range current.doorList {
            namedSet[rowDoor.anchor] = true
        }
    }

    for _, key := range sortedKeyList(measured.itemMap) {
        if true == namedSet[key] {
            continue
        }
        measuredItem := measured.itemMap[key]
        fmt.Printf("%s\t%s\t%s\t%t\t%t\t%s\t%s\n", measuredItem.kind, key, measuredItem.position, measuredItem.unlimitedAdmitted, measuredItem.hasMaximum, strings.TrimSpace(measuredItem.boundText), measured.suggestDoor(key))
    }

    for _, field := range sortedKeyList(measured.doorFieldMap) {
        if false == namedSet[field] {
            fmt.Printf("%s\t%s\t%s\t%s\n", kindDoor, field, measured.doorFieldMap[field], measured.doorFieldTextMap[field])
        }
    }
}

/* suggestDoor answers the field a rule's name resolves to, in its own package first, then in any package of its module, for a draft row */
func (instance *inventory) suggestDoor(key string) string {
    lastSlash := strings.LastIndex(key, "/")
    firstDot := strings.Index(key[lastSlash+1:], ".")
    if -1 == firstDot {
        return ""
    }

    packagePath := key[:lastSlash+1+firstDot]
    name := key[lastSlash+1+firstDot+1:]
    if true == instance.resolveAnchor(packagePath + "." + name) {
        return packagePath + "." + name
    }

    moduleRoot := packagePath
    if index := strings.Index(packagePath, "/v4"); -1 != index {
        moduleRoot = packagePath[:index+3]
    }
    if strings.HasPrefix(packagePath, "v4") {
        moduleRoot = "v4"
    }

    for _, candidate := range sortedKeyList(instance.packageMap) {
        if (candidate == moduleRoot || true == strings.HasPrefix(candidate, moduleRoot+"/")) && true == instance.resolveAnchor(candidate+"."+name) {
            return candidate + "." + name
        }
    }

    return ""
}

func sortedKeyList[V any](source map[string]V) []string {
    keyList := make([]string, 0, len(source))
    for key := range source {
        keyList = append(keyList, key)
    }
    sort.Strings(keyList)

    return keyList
}
