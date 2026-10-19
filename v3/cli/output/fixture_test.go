/* The shared test material of this package: the command line driver the flag and option tests reach for. It is the ONE test file of the package allowed to exist without a matching source. */
package output

import (
    "context"
    "io"
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
)

/* runStandardFlags drives one command line through the engine over StandardFlags and answers the parsed command and the engine's refusal: the flags are the engine's own types, so what is proved is what a command declaring them receives. */
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
