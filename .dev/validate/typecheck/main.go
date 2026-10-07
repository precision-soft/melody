package main

import (
    "fmt"
    "os"
    "sort"
    "strings"

    "golang.org/x/tools/go/packages"
)

/* main type-checks every package of the module in the working directory against the versions its go.mod pins, past a package that fails: go build stops at the first failing package of an import chain and checks none of its importers, while the type checker checks an importer against the partial package it imports. Each package is printed as CHECKED, each error in the compiler's file:line:column shape, so the compatibility band reads both with the reader it already has. */
func main() {
    configuration := &packages.Config{
        Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
        Env:  append(os.Environ(), "GOWORK=off"),
    }

    loadedPackageList, loadErr := packages.Load(configuration, "./...")
    if nil != loadErr {
        fmt.Fprintf(os.Stderr, "load the module's packages: %v\n", loadErr)
        os.Exit(2)
    }

    sort.Slice(loadedPackageList, func(left int, right int) bool {
        return loadedPackageList[left].PkgPath < loadedPackageList[right].PkgPath
    })

    errorCount := 0
    for _, loadedPackage := range loadedPackageList {
        fmt.Printf("CHECKED %s\n", loadedPackage.PkgPath)

        /* a package that fails to compile also carries the go list error, which repeats the compiler's diagnostics; the type checker's own errors are the ones read, and a list error is printed only when the package has nothing else, on one line */
        hasTypeError := false
        for _, packageError := range loadedPackage.Errors {
            if packages.TypeError == packageError.Kind || packages.ParseError == packageError.Kind {
                hasTypeError = true
            }
        }

        for _, packageError := range loadedPackage.Errors {
            errorCount = errorCount + 1

            if packages.TypeError == packageError.Kind || packages.ParseError == packageError.Kind {
                fmt.Printf("%s: %s\n", packageError.Pos, packageError.Msg)

                continue
            }

            if false == hasTypeError {
                fmt.Printf("LISTERROR %s: %s\n", loadedPackage.PkgPath, strings.Join(strings.Fields(packageError.Msg), " "))
            }
        }
    }

    if 0 < errorCount {
        os.Exit(1)
    }
}
