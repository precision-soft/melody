package output

import (
    "context"
    "errors"
    "io"
    "testing"

    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/exception"
)

func runStandardFlags(t *testing.T, arguments ...string) (*clicontract.CommandContext, error) {
    t.Helper()

    var parsed *clicontract.CommandContext

    commandContext := &clicontract.CommandContext{
        Name:      "test",
        Flags:     StandardFlags(),
        Writer:    io.Discard,
        ErrWriter: io.Discard,
        Action: func(ctx context.Context, actionCommand *clicontract.CommandContext) error {
            parsed = actionCommand

            return nil
        },
        ExitErrHandler: func(context.Context, *clicontract.CommandContext, error) {},
    }

    runErr := commandContext.Run(context.Background(), append([]string{"test"}, arguments...))

    return parsed, runErr
}

func assertRefusedOptionField(t *testing.T, err error, field string) {
    t.Helper()

    var refusal *exception.Error
    if false == errors.As(err, &refusal) {
        t.Fatalf("expected a melody error naming %q, got %v", field, err)
    }

    if field != refusal.Context()["field"] {
        t.Fatalf("expected the refusal to name %q, got %+v", field, refusal.Context())
    }
}
