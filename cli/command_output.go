package cli

import (
    "errors"
    "fmt"
    "io"
    "os"
    "sort"

    exceptioncontract "github.com/precision-soft/melody/exception/contract"
)

type commandOutput struct {
    errWriter io.Writer
}

func newCommandOutput(errWriter io.Writer) *commandOutput {
    resolvedWriter := errWriter
    if nil == resolvedWriter {
        resolvedWriter = os.Stderr
    }

    return &commandOutput{errWriter: resolvedWriter}
}

func (instance *commandOutput) printError(err error) {
    if nil == err {
        return
    }

    writer := instance.errWriter
    if nil == writer {
        writer = os.Stderr
    }

    _, _ = fmt.Fprintln(writer)
    _, _ = fmt.Fprintf(
        writer,
        "%serror:%s %s\n",
        AnsiRed,
        AnsiReset,
        err.Error(),
    )

    contextProvider, isContextProvider := err.(exceptioncontract.ContextProvider)
    if true == isContextProvider {
        context := contextProvider.Context()
        if 0 < len(context) {
            keys := make([]string, 0, len(context))
            for key := range context {
                if "" == key {
                    continue
                }
                keys = append(keys, key)
            }

            sort.Strings(keys)

            for _, key := range keys {
                _, _ = fmt.Fprintf(
                    writer,
                    "  %s: %v\n",
                    key,
                    context[key],
                )
            }
        }
    }

    causeErr := errors.Unwrap(err)
    for i := 0; i < 4 && nil != causeErr; i++ {
        _, _ = fmt.Fprintf(
            writer,
            "%scause:%s %s\n",
            AnsiRed,
            AnsiReset,
            causeErr.Error(),
        )

        causeErr = errors.Unwrap(causeErr)
    }
}
