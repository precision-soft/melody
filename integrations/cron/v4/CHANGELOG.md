# Changelog

All notable changes to `precision-soft/melody/integrations/cron/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major up to v3.8.0; that history is in the third major's changelog.
- `runner_config.go`: `RunnerConfig.OutputCaptureLimit` bounds what one run writes into the journal, 64 KiB when left out as the constant was, at least 1, unlimited refused; `RunnerConfig.ClockJumpResetThreshold` is the wall-clock jump that re-anchors the minute chain, three hours when left out as the constant was, at least two minutes, in whole minutes, unlimited refused; `RunnerConfig.Timeout` and `RunnerConfig.GracefulTimeout` are what an entry that names no timeout or window of its own runs under.

### Changed

- `runner_command.go`: **Behavioural change**: the child scope of a run closes under the container's `ScopeTeardownTimeout`, 3s when left out, where a scoped service waiting on its context held the runner without limit; a service that did not finish is reported as the run's close failure.
- `runner_command.go`: the log records are built through `exception.LogContext(err, exception.LogContextConfig{...})`, the fourth major's form, which takes the extra context in a struct; nothing of this module's own surface changes.
- **Breaking** — `runner_command.go`, `runner_config.go`, `configuration.go`, `module.go`: `NewRunnerCommand` takes the configuration and one `RunnerConfig` of `Dialect`, `Commands`, `OutputCaptureLimit`, `ClockJumpResetThreshold`, `Timeout` and `GracefulTimeout`, resolved once, a value outside its rule panicking naming the field, where it took the dialect and a variadic command list; `ModuleConfig.Runner` replaces `RunnerCommands` and `RunnerDialect`. `Configuration.Schedule` takes the `EntryConfig` by value and `ScheduledCommand.Config` is a value; `EntryConfig.Instances` is a `bound.Count`, 1 when left out, at least 1, unlimited refused, a negative one refused naming the field by the generator and by `NewRunnerCommand` where it was read as 1; `EntryConfig.Timeout` and `EntryConfig.GracefulTimeout` are `bound.Duration`, the runner's when left out, at least 1ms, an unlimited timeout admitted, an unlimited window refused, an entry refusal naming the field and the command, where a negative timeout meant no deadline and a non-positive window the default.
- **Behavioural change** — `runner_config.go`: an entry that names no timeout is cancelled after one hour, `RunnerConfig.Timeout`'s default, where it ran with no deadline; an entry that wants none writes `Timeout: bound.UnlimitedDuration()`, and a runner that wants none for every such entry writes it on `RunnerConfig.Timeout`.

### Removed

- **Breaking** — `validation.go`: the deprecated aliases `ForbiddenChar`, `CrontabForbiddenChars` and `ValidateNoForbiddenChars` are removed; `ForbiddenCharacter`, `CrontabForbiddenCharacters` and `ValidateNoForbiddenCharacters` remain.

### Fixed

- runner: a sibling failure's cause chain renders each pointer link once, breadth-first, as the framework's chain does, where a chain reaching itself repeated its links up to the bound of eight.
- runner: a scheduled command's error whose `Unwrap` chain reaches itself no longer holds the dispatch worker that classifies it, nor the command that renders it.

