package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	clicontract "github.com/precision-soft/melody/v2/cli/contract"
	"github.com/precision-soft/melody/v2/exception"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func NewCommandContext(applicationName string, applicationDescription string) *clicontract.CommandContext {
	commandContext := &clicontract.CommandContext{
		Name:  applicationName,
		Usage: applicationDescription,
	}

	return commandContext
}

func Register(commandContext *clicontract.CommandContext, command clicontract.Command, runtimeInstance runtimecontract.Runtime) {
	if nil == commandContext {
		exception.Panic(
			exception.NewError("root cli command may not be nil", nil, nil),
		)
	}

	if nil == command {
		exception.Panic(
			exception.NewError("cli command may not be nil", nil, nil),
		)
	}

	if nil == runtimeInstance {
		exception.Panic(
			exception.NewError("runtime instance may not be nil in cli register", nil, nil),
		)
	}

	copied := command

	commandName := copied.Name()
	normalizedCommandName := strings.TrimSpace(commandName)

	if "" == normalizedCommandName {
		exception.Panic(
			exception.NewError(
				"cli command name may not be empty",
				map[string]any{
					"commandName": commandName,
				},
				nil,
			),
		)
	}

	for _, existing := range commandContext.Commands {
		if nil == existing {
			continue
		}

		if normalizedCommandName == strings.TrimSpace(existing.Name) {
			exception.Panic(
				exception.NewError(
					"cli command name already registered",
					map[string]any{
						"commandName": normalizedCommandName,
					},
					nil,
				),
			)
		}
	}

	commandContext.Commands = append(
		commandContext.Commands,
		&clicontract.CommandContext{
			Name:  normalizedCommandName,
			Usage: copied.Description(),
			Flags: copied.Flags(),
			Action: func(ctx context.Context, commandContext *clicontract.CommandContext) error {
				writer := commandContext.Writer
				if nil == writer {
					writer = io.Discard
				}

				startedAt := time.Now()
				const logFiller = "======================================"
				printGreenFullLine := func(writer io.Writer) {
					_, _ = fmt.Fprintf(
						writer,
						"%s%s%s\n",
						AnsiBackgroundGreen,
						AnsiEraseLine,
						AnsiReset,
					)
				}

				printGreenStatusLine := func(writer io.Writer, text string) {
					_, _ = fmt.Fprintf(
						writer,
						"%s%s\r%s%s%s\n",
						AnsiBackgroundGreen,
						AnsiEraseLine,
						AnsiWhite,
						text,
						AnsiReset,
					)
				}

				printGreenFullLine(writer)

				printGreenStatusLine(
					writer,
					fmt.Sprintf(
						"%s [%s] [started] [%s] %s",
						logFiller,
						normalizedCommandName,
						startedAt.Format(time.DateTime),
						logFiller,
					),
				)

				printGreenFullLine(writer)

				defer func() {
					finishedAt := time.Now()
					duration := finishedAt.Sub(startedAt)

					durationSecondsString := fmt.Sprintf("%.3fs", duration.Seconds())

					printGreenFullLine(writer)

					printGreenStatusLine(
						writer,
						fmt.Sprintf(
							"%s [%s] [finished] [%s] [duration=%s] %s",
							logFiller,
							normalizedCommandName,
							finishedAt.Format(time.DateTime),
							durationSecondsString,
							logFiller,
						),
					)

					printGreenFullLine(writer)
				}()

				runErr := copied.Run(runtimeInstance, commandContext)

				closeErrorByName := map[string]error{}

				scopeCloseErr := runtimeInstance.Scope().Close()
				if nil != scopeCloseErr {
					closeErrorByName["scope"] = scopeCloseErr
				}

				containerCloseErr := runtimeInstance.Container().Close()
				if nil != containerCloseErr {
					closeErrorByName["container"] = containerCloseErr
				}

				aggregatedErr := aggregateCliErrors(runErr, closeErrorByName)
				if nil != aggregatedErr {
					return aggregatedErr
				}

				return nil
			},
		},
	)
}

func aggregateCliErrors(runErr error, closeErrorByName map[string]error) error {
	if 0 == len(closeErrorByName) {
		return runErr
	}

	keys := make([]string, 0, len(closeErrorByName))
	for key := range closeErrorByName {
		if "" == key {
			continue
		}

		keys = append(keys, key)
	}

	sort.Strings(keys)

	failures := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		err := closeErrorByName[key]
		if nil == err {
			continue
		}

		failures = append(
			failures,
			map[string]string{
				"name":    key,
				"message": err.Error(),
			},
		)
	}

	if 0 == len(failures) {
		return runErr
	}

	if nil == runErr {
		return exception.NewError(
			"failed to shutdown cli",
			map[string]any{
				"failures": failures,
			},
			nil,
		)
	}

	return exception.NewError(
		"cli command failed with shutdown errors",
		map[string]any{
			"runError":    runErr.Error(),
			"failures":    failures,
			"hasFailures": true,
		},
		runErr,
	)
}
