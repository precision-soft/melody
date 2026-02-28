package application

import (
    "context"
    "fmt"
    "io"
    "os"
    "reflect"
    "sort"
    "strings"
    "time"

    "github.com/precision-soft/melody/cli"
    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/cli/output"
    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/debug"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/logging"
    "github.com/precision-soft/melody/runtime"
    "github.com/precision-soft/melody/version"
)

type commandSuggestion struct {
    Name        string
    Description string
}

func (instance *Application) RegisterCliCommand(command clicontract.Command) {
    if true == instance.booted {
        exception.Panic(
            exception.NewError(
                "cannot register cli commands after boot",
                nil,
                nil,
            ),
        )
    }

    if nil == command {
        exception.Panic(
            exception.NewError(
                "cli command may not be nil",
                nil,
                nil,
            ),
        )
    }

    commandName := command.Name()
    if "" == commandName {
        exception.Panic(
            exception.NewError(
                "cli command name may not be empty",
                exceptioncontract.Context{
                    "commandType": reflect.TypeOf(command).String(),
                },
                nil,
            ),
        )
    }

    for _, existingCommand := range instance.cliCommands {
        if commandName == existingCommand.Name() {
            exception.Panic(
                exception.NewError(
                    "duplicate cli command name",
                    exceptioncontract.Context{
                        "commandName": commandName,
                        "existing":    reflect.TypeOf(existingCommand).String(),
                        "new":         reflect.TypeOf(command).String(),
                    },
                    nil,
                ),
            )
        }
    }

    instance.cliCommands = append(instance.cliCommands, command)
}

func (instance *Application) bootCli() {
    debugCommands := []clicontract.Command{
        &debug.RouterCommand{},
    }

    if config.EnvDevelopment == instance.configuration.Kernel().Env() {
        debugCommands = append(
            debugCommands,
            &debug.ContainerCommand{},
            &debug.ParameterCommand{},
            &debug.EventCommand{},
            debug.NewMiddlewareCommand(func() []httpcontract.Middleware {
                return instance.httpMiddlewares.all(instance.kernel)
            }),
            &debug.VersionCommand{ApplicationVersion: version.BuildVersion()},
        )
    }

    for _, commandInstance := range debugCommands {
        instance.RegisterCliCommand(commandInstance)
    }
}

func (instance *Application) runCli(ctx context.Context) error {
    kernelInstance := instance.kernel
    configuration := instance.configuration

    serviceContainer := kernelInstance.ServiceContainer()

    logger := logging.LoggerMustFromContainer(serviceContainer)

    scope := serviceContainer.NewScope()
    defer func() {
        scopeCloseErr := scope.Close()
        if nil != scopeCloseErr {
            logging.EmergencyLogger().Error("failed to close service container scope", exception.LogContext(scopeCloseErr))
        }
    }()

    runtimeInstance := runtime.New(ctx, scope, serviceContainer)

    processId := logging.GenerateProcessId()
    loggerWithProcess := logging.NewRequestLogger(logger, processId, "processId")

    scope.MustOverrideProtectedInstance(logging.ServiceLogger, loggerWithProcess)

    loggerWithProcess.Info("starting cli application", nil)

    rootCli := cli.NewCommandContext(configuration.Cli().Name(), configuration.Cli().Description())
    rootCli.Writer = os.Stdout
    rootCli.ErrWriter = os.Stderr

    availableCommands := make([]commandSuggestion, 0, len(instance.cliCommands))

    for _, command := range instance.cliCommands {
        availableCommands = append(
            availableCommands,
            commandSuggestion{
                Name:        command.Name(),
                Description: command.Description(),
            },
        )

        cli.Register(rootCli, command, runtimeInstance)
    }

    normalizedArguments := normalizeCliVerbosityArguments(os.Args)

    suggestCliCommandErr := suggestCliCommand(normalizedArguments, availableCommands)
    if nil != suggestCliCommandErr {
        return suggestCliCommandErr
    }

    return rootCli.Run(ctx, normalizedArguments)
}

func normalizeCliVerbosityArguments(arguments []string) []string {
    if 0 == len(arguments) {
        return arguments
    }

    normalized := make([]string, 0, len(arguments))
    stopNormalization := false

    for _, argument := range arguments {
        if true == stopNormalization {
            normalized = append(normalized, argument)
            continue
        }

        if "--" == argument {
            stopNormalization = true
            normalized = append(normalized, argument)
            continue
        }

        if true == strings.HasPrefix(argument, "-") && false == strings.HasPrefix(argument, "--") {
            isVerbosityShortFlag := true

            if 2 > len(argument) {
                isVerbosityShortFlag = false
            }

            if true == isVerbosityShortFlag && false == strings.HasPrefix(argument, "-v") {
                isVerbosityShortFlag = false
            }

            if true == isVerbosityShortFlag {
                for _, runeValue := range argument[2:] {
                    if 'v' != runeValue {
                        isVerbosityShortFlag = false
                        break
                    }
                }
            }

            if true == isVerbosityShortFlag {
                verbosityLevel := len(argument) - 1
                normalized = append(
                    normalized,
                    fmt.Sprintf(
                        "--%s=%d",
                        output.FlagNameVerbosity,
                        verbosityLevel,
                    ),
                )

                continue
            }
        }

        normalized = append(normalized, argument)
    }

    return normalized
}

func suggestCliCommand(
    arguments []string,
    availableCommands []commandSuggestion,
) error {
    if 2 > len(arguments) {
        return nil
    }

    commandName := strings.TrimSpace(arguments[1])
    if "" == commandName {
        return nil
    }

    if true == strings.HasPrefix(commandName, "-") {
        return nil
    }

    lowercaseCommandName := strings.ToLower(commandName)

    if "help" == lowercaseCommandName || "h" == lowercaseCommandName {
        return nil
    }

    for _, availableCommand := range availableCommands {
        if availableCommand.Name == commandName {
            return nil
        }
    }

    startedAt := time.Now()

    option := output.NormalizeOption(
        output.Option{
            Format:  output.FormatTable,
            NoColor: true,
            Verbose: false,
            Quiet:   true,
            Fields:  []string{},
            SortKey: "",
            Order:   output.SortOrderAscending,
            Limit:   0,
            Offset:  0,
        },
    )

    meta := output.NewMeta(
        "cli:suggest",
        []string{commandName},
        option,
        startedAt,
        time.Duration(0),
        output.Version{},
    )

    envelope := output.NewEnvelope(meta)

    matchingCommands := make([]commandSuggestion, 0, len(availableCommands))

    for _, availableCommand := range availableCommands {
        if strings.Contains(strings.ToLower(availableCommand.Name), lowercaseCommandName) {
            matchingCommands = append(matchingCommands, availableCommand)
        }
    }

    sort.Slice(matchingCommands, func(leftIndex int, rightIndex int) bool {
        return matchingCommands[leftIndex].Name < matchingCommands[rightIndex].Name
    })

    builder := output.NewTableBuilder()

    const maxMatchesToPrint = 50

    if 0 == len(matchingCommands) {
        envelope.SetError(
            "cli.commandNotFound",
            "cli command not found",
            map[string]any{
                "command": commandName,
            },
            nil,
        )

        builder.AddSummaryLine("MATCHES: 0")

        block := builder.AddBlock(
            "AVAILABLE COMMANDS",
            []string{"command", "description"},
        )

        for _, availableCommand := range availableCommands {
            description := strings.TrimSpace(availableCommand.Description)
            if "" == description {
                description = "-"
            }

            block.AddRow(
                availableCommand.Name,
                description,
            )
        }

        envelope.Table = builder.Build()

        envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

        printCliCommandNotFoundHeader(os.Stderr, commandName, startedAt)

        _ = output.Render(os.Stderr, envelope, option)

        commandNotFoundErr := exception.NewError(
            "cli command not found",
            exceptioncontract.Context{
                "command": commandName,
            },
            nil,
        )
        _ = exception.MarkLogged(commandNotFoundErr)

        return exception.NewExitError(2, commandNotFoundErr)
    }

    matchesToPrint := matchingCommands
    if maxMatchesToPrint < len(matchingCommands) {
        matchesToPrint = matchingCommands[:maxMatchesToPrint]
    }

    envelope.SetError(
        "cli.commandNotFound",
        "cli command not found, matches found",
        map[string]any{
            "command":        commandName,
            "matchesTotal":   len(matchingCommands),
            "matchesPrinted": len(matchesToPrint),
        },
        nil,
    )

    builder.AddSummaryLine(
        fmt.Sprintf(
            "MATCHES: %d total | %d shown",
            len(matchingCommands),
            len(matchesToPrint),
        ),
    )

    block := builder.AddBlock(
        "SUGGESTED COMMANDS",
        []string{"command", "description"},
    )

    for _, match := range matchesToPrint {
        description := strings.TrimSpace(match.Description)
        if "" == description {
            description = "-"
        }

        block.AddRow(
            match.Name,
            description,
        )
    }

    if maxMatchesToPrint < len(matchingCommands) {
        builder.AddSummaryLine(
            fmt.Sprintf(
                "... and %d more",
                len(matchingCommands)-maxMatchesToPrint,
            ),
        )
    }

    envelope.Table = builder.Build()

    envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

    printCliCommandNotFoundHeader(os.Stderr, commandName, startedAt)

    _ = output.Render(os.Stderr, envelope, option)

    matchesFoundErr := exception.NewError(
        "cli command not found, matches found",
        exceptioncontract.Context{
            "command":        commandName,
            "matchesTotal":   len(matchingCommands),
            "matchesPrinted": len(matchesToPrint),
        },
        nil,
    )

    _ = exception.MarkLogged(matchesFoundErr)

    return exception.NewExitError(2, matchesFoundErr)
}

func printCliCommandNotFoundHeader(writer io.Writer, commandName string, startedAt time.Time) {
    const logFiller = "======================================"

    printGreenFullLine := func(writer io.Writer) {
        _, _ = fmt.Fprintf(
            writer,
            "%s%s%s\n",
            cli.AnsiBackgroundGreen,
            cli.AnsiEraseLine,
            cli.AnsiReset,
        )
    }

    printGreenStatusLine := func(writer io.Writer, text string) {
        _, _ = fmt.Fprintf(
            writer,
            "%s%s\r%s%s%s\n",
            cli.AnsiBackgroundGreen,
            cli.AnsiEraseLine,
            cli.AnsiWhite,
            text,
            cli.AnsiReset,
        )
    }

    printGreenFullLine(writer)

    printGreenStatusLine(
        writer,
        fmt.Sprintf(
            "%s [command not found] [%s] [%s] %s",
            logFiller,
            commandName,
            startedAt.Format(time.DateTime),
            logFiller,
        ),
    )

    printGreenFullLine(writer)
}
