package main

import (
    "fmt"
    "go/types"
    "os"
    "path/filepath"
    "sort"
    "strings"

    "golang.org/x/tools/go/packages"
)

/* interfaceEntry is one exported interface of the module: its name as the contract list spells it, `<package path relative to the module>.<Name>` or the bare name for the root package, and the file that declares it, relative to the module directory. */
type interfaceEntry struct {
    name string
    file string
}

/* main prints every exported named interface of the module in the working directory, one `<name>\t<file>` line each, sorted, for the contract completeness check of .dev/validate/apidiff.sh. The module loads under whatever workspace the caller hands it, so an integration ahead of its pins still type-checks; a package that does not type-check fails the run, because a list read off a broken package is a list nobody can trust to be whole. */
func main() {
    workingDirectory, workingDirectoryErr := os.Getwd()
    if nil != workingDirectoryErr {
        fmt.Fprintf(os.Stderr, "read the working directory: %v\n", workingDirectoryErr)
        os.Exit(2)
    }

    entryList, listErr := listInterfaces(workingDirectory, os.Environ())
    if nil != listErr {
        fmt.Fprintf(os.Stderr, "%v\n", listErr)
        os.Exit(1)
    }

    for _, entry := range entryList {
        fmt.Printf("%s\t%s\n", entry.name, entry.file)
    }
}

/* listInterfaces loads every package of the module rooted at moduleDirectory and returns its exported named interfaces. Out: unexported names, a package under an internal/ directory (no importer outside the module can reach it), a main package, a test file, a package of another module the workspace nests beneath this one, and an interface that is a type set (a constraint only, which nothing implements). In: a generic interface, which is implemented like any other. An alias is resolved to the type it names: an alias of an interface the module declares itself adds no entry, the declaration is the entry; an alias of an interface declared outside the module, or under its internal/ directory, is the module's own name for that contract and is listed under the alias. */
func listInterfaces(moduleDirectory string, environment []string) ([]interfaceEntry, error) {
    absoluteModuleDirectory, absoluteErr := filepath.Abs(moduleDirectory)
    if nil != absoluteErr {
        return nil, fmt.Errorf("resolve the module directory %s: %w", moduleDirectory, absoluteErr)
    }

    /* the syntax is loaded so the packages are type-checked from source: types read from export data hold only what the package exports, which would hide from the exported check below the very declarations it exists to refuse */
    configuration := &packages.Config{
        Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule,
        Dir:  absoluteModuleDirectory,
        Env:  environment,
    }

    loadedPackageList, loadErr := packages.Load(configuration, "./...")
    if nil != loadErr {
        return nil, fmt.Errorf("load the module's packages: %w", loadErr)
    }

    var modulePath string
    for _, loadedPackage := range loadedPackageList {
        if nil != loadedPackage.Module && absoluteModuleDirectory == filepath.Clean(loadedPackage.Module.Dir) {
            modulePath = loadedPackage.Module.Path

            break
        }
    }
    if "" == modulePath {
        return nil, fmt.Errorf("no package of %s belongs to the module rooted there", absoluteModuleDirectory)
    }

    errorList := []string{}
    entryList := []interfaceEntry{}
    for _, loadedPackage := range loadedPackageList {
        if nil == loadedPackage.Module || modulePath != loadedPackage.Module.Path {
            continue
        }

        for _, packageError := range loadedPackage.Errors {
            errorList = append(errorList, fmt.Sprintf("%s: %s", loadedPackage.PkgPath, packageError.Error()))
        }

        relativePackagePath := strings.TrimPrefix(strings.TrimPrefix(loadedPackage.PkgPath, modulePath), "/")
        if true == isInternalPath(relativePackagePath) || "main" == loadedPackage.Name || nil == loadedPackage.Types {
            continue
        }

        scope := loadedPackage.Types.Scope()
        for _, objectName := range scope.Names() {
            typeName, isTypeName := scope.Lookup(objectName).(*types.TypeName)
            if false == isTypeName || false == typeName.Exported() {
                continue
            }

            if false == isListedInterface(typeName, modulePath) {
                continue
            }

            declaringFile := loadedPackage.Fset.Position(typeName.Pos()).Filename
            relativeFile, relativeErr := filepath.Rel(absoluteModuleDirectory, declaringFile)
            if nil != relativeErr {
                relativeFile = declaringFile
            }

            entryName := objectName
            if "" != relativePackagePath {
                entryName = relativePackagePath + "." + objectName
            }

            entryList = append(entryList, interfaceEntry{name: entryName, file: filepath.ToSlash(relativeFile)})
        }
    }

    if 0 < len(errorList) {
        return nil, fmt.Errorf("the module does not type-check, so its interface list cannot be read whole:\n%s", strings.Join(errorList, "\n"))
    }

    sort.Slice(entryList, func(left int, right int) bool {
        return entryList[left].name < entryList[right].name
    })

    return entryList, nil
}

/* isListedInterface answers whether an exported type name is a contract row: a named interface that is a method set, declared here, or an alias of one declared outside the module or under its internal/ directory. */
func isListedInterface(typeName *types.TypeName, modulePath string) bool {
    resolvedType := types.Unalias(typeName.Type())

    interfaceType, isInterface := resolvedType.Underlying().(*types.Interface)
    if false == isInterface || false == interfaceType.IsMethodSet() {
        return false
    }

    if false == typeName.IsAlias() {
        return true
    }

    namedType, isNamed := resolvedType.(*types.Named)
    if false == isNamed || nil == namedType.Obj().Pkg() {
        return false
    }

    targetPackagePath := namedType.Obj().Pkg().Path()
    if modulePath != targetPackagePath && false == strings.HasPrefix(targetPackagePath, modulePath+"/") {
        return true
    }

    /* an alias of an interface the module keeps under internal/ is the only name an importer can reach it by */
    return isInternalPath(strings.TrimPrefix(strings.TrimPrefix(targetPackagePath, modulePath), "/"))
}

/* isInternalPath answers whether a package path relative to the module sits under an internal/ directory, which the go tool refuses to every importer outside the module. */
func isInternalPath(relativePackagePath string) bool {
    for _, segment := range strings.Split(relativePackagePath, "/") {
        if "internal" == segment {
            return true
        }
    }

    return false
}
