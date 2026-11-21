package main

import (
    "fmt"
    "os"
    "strings"

    "github.com/precision-soft/melody/v4/.example/wiring"
    melodywiring "github.com/precision-soft/melody/v4/wiring"
)

const (
    generatedFile     = "wiring_gen.go"
    generatedPackage  = "generated"
    generatedFunction = "RegisterGeneratedServices"
    /* go generate runs this from the generated package, one level below the application root */
    projectDirectory = ".."
)

/* main writes generated/wiring_gen.go, which the tree does not commit. The application's own melody:wiring:generate renders the same file, but it is compiled into an application that imports the file it writes, so it cannot run on a tree that does not hold one yet; this program imports the bind set alone. It refuses what the command's --strict refuses: a constructor the scan skipped, a bind or an exclude that matched nothing. */
func main() {
    source, report, generateErr := melodywiring.Generate(&melodywiring.GenerateRequest{
        ProjectDirectory: projectDirectory,
        PackageName:      generatedPackage,
        FunctionName:     generatedFunction,
        BindSet:          wiring.NewBindSet(),
    })
    if nil != generateErr {
        exit("the wiring did not generate: %v", generateErr)
    }

    if 0 < len(report.Skipped) {
        skippedNames := make([]string, 0, len(report.Skipped))
        for _, skipped := range report.Skipped {
            skippedNames = append(skippedNames, skipped.Name)
        }

        exit("the scan skipped constructors: %s", strings.Join(skippedNames, ", "))
    }

    if 0 < len(report.UnusedBinds) {
        exit("binds matched no constructor: %s", strings.Join(report.UnusedBinds, ", "))
    }

    if 0 < len(report.UnusedExcludes) {
        exit("excludes matched no constructor: %s", strings.Join(report.UnusedExcludes, ", "))
    }

    /* written beside the target and renamed over it, so a build reading the package meanwhile sees a whole file, never a part; the dot keeps the go tool from reading the temporary as a source */
    temporaryFile, createErr := os.CreateTemp(".", "."+generatedFile+"-*")
    if nil != createErr {
        exit("could not create the temporary file: %v", createErr)
    }

    _, writeErr := temporaryFile.WriteString(source)
    closeErr := temporaryFile.Close()
    /* CreateTemp opens the file 0600; the generated source is read like every other one */
    chmodErr := os.Chmod(temporaryFile.Name(), 0o644)
    if nil != writeErr || nil != closeErr || nil != chmodErr {
        _ = os.Remove(temporaryFile.Name())
        exit("could not write %s: %v", temporaryFile.Name(), firstError(writeErr, closeErr, chmodErr))
    }

    if renameErr := os.Rename(temporaryFile.Name(), generatedFile); nil != renameErr {
        _ = os.Remove(temporaryFile.Name())
        exit("could not write %s: %v", generatedFile, renameErr)
    }
}

func firstError(errs ...error) error {
    for _, err := range errs {
        if nil != err {
            return err
        }
    }

    return nil
}

func exit(format string, arguments ...any) {
    fmt.Fprintf(os.Stderr, "wiring/generate: "+format+"\n", arguments...)
    os.Exit(1)
}
