# Changelog

All notable changes to `precision-soft/melody/integrations/cron/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `parameter.go`, `module.go`: the module declares its ten `.env` keys through the framework's `EnvironmentKeyModule`, `MELODY_CRON_USER`, `MELODY_CRON_LOGS_DIR`, `MELODY_CRON_BINARY`, `MELODY_CRON_DESTINATION_FILE`, `MELODY_CRON_HEARTBEAT_PATH`, `MELODY_CRON_HEARTBEAT_ENABLED`, `MELODY_CRON_TEMPLATE`, `MELODY_CRON_K8S_IMAGE`, `MELODY_CRON_K8S_NAMESPACE` and `MELODY_CRON_K8S_RESTART_POLICY`, each one parameter with its `melody.cron.*` name and taking `ModuleConfig.KeyDefaults` or the module's default when no `.env` file sets it; the heartbeat switch, the RFC 1123 namespace and the `OnFailure` or `Never` restart policy are judged at boot in every mode the module is registered in, refused naming the key and the `cron.KeyDefaults` field, where they were judged only when `melody:cron:generate` ran; `EnvironmentKeys` and `KeyDefaults` for an application module that builds `NewGenerateCommand` without the cron module.
- This major includes every change of the third major up to v3.8.0; that history is in the third major's changelog.
- `runner_config.go`: `RunnerConfig.OutputCaptureLimit` bounds what one run writes into the journal, 64 KiB when left out as the constant was, at least 1, unlimited refused; `RunnerConfig.ClockJumpResetThreshold` is the wall-clock jump that re-anchors the minute chain, three hours when left out as the constant was, at least two minutes, in whole minutes, unlimited refused; `RunnerConfig.Timeout` and `RunnerConfig.GracefulTimeout` are what an entry that names no timeout or window of its own runs under.

### Changed

- `runner_command.go`: a scheduled command's document carries the application's version: the runner reads it off the tree it was started under and hands it to each dispatch, which runs on the process's context under no tree of its own and read it empty.
- `module.go`, `parameter.go`: **Behavioural change**: the module's defaults are no longer opt-in: with no `--out` the generator writes `%kernel.project_dir%/generated_conf/cron/crontab` and with no `--logs-dir` it redirects every entry under `%kernel.logs_dir%/cron`, creating that directory on every run, the k8s template's included, where a module registered without `WithDefaultParameters` refused both runs naming the flag; an empty `.env` line for `MELODY_CRON_DESTINATION_FILE` or `MELODY_CRON_LOGS_DIR` restores the refusal.
- `generate_command.go`: **Behavioural change**: the heartbeat opt-in reads an empty value as unset, the declared key's default, where an empty parameter was refused as not holding a boolean.
- `runner_command.go`: **Behavioural change**: the child scope of a run closes under the container's `ScopeTeardownTimeout`, 3s when left out, where a scoped service waiting on its context held the runner without limit; a service that did not finish is reported as the run's close failure.
- `runner_command.go`: the log records are built through `exception.LogContext(err, exception.LogContextConfig{...})`, the fourth major's form, which takes the extra context in a struct; nothing of this module's own surface changes.
- **Breaking** — `runner_command.go`, `runner_config.go`, `configuration.go`, `module.go`: `NewRunnerCommand` takes the configuration and one `RunnerConfig` of `Dialect`, `Commands`, `OutputCaptureLimit`, `ClockJumpResetThreshold`, `Timeout` and `GracefulTimeout`, resolved once, a value outside its rule panicking naming the field, where it took the dialect and a variadic command list; `ModuleConfig.Runner` replaces `RunnerCommands` and `RunnerDialect`. `Configuration.Schedule` takes the `EntryConfig` by value and `ScheduledCommand.Config` is a value; `EntryConfig.Instances` is a `bound.Count`, 1 when left out, at least 1, unlimited refused, a negative one refused naming the field by the generator and by `NewRunnerCommand` where it was read as 1; `EntryConfig.Timeout` and `EntryConfig.GracefulTimeout` are `bound.Duration`, the runner's when left out, at least 1ms, an unlimited timeout admitted, an unlimited window refused, an entry refusal naming the field and the command, where a negative timeout meant no deadline and a non-positive window the default.
- **Behavioural change** — `runner_config.go`: an entry that names no timeout is cancelled after one hour, `RunnerConfig.Timeout`'s default, where it ran with no deadline; an entry that wants none writes `Timeout: bound.UnlimitedDuration()`, and a runner that wants none for every such entry writes it on `RunnerConfig.Timeout`.
- **Breaking** — `entry.go`, `template_k8s.go`: `Entry.Args` is renamed `Entry.Arguments`, and the k8s template's refusal of an empty token names the field `Arguments`; nothing else changes.

### Removed

- **Breaking** — `parameter.go`, `module.go`: `ParameterRegistrar`, `RegisterDefaultParameters` and `ModuleConfig.WithDefaultParameters`; the module no longer implements `ParameterModule`; a parameter of one of the ten declared names that the application registers before `Boot` is refused naming the key that feeds it, and one a parameter hook registers is the duplicate of the aggregated boot report.
- **Breaking** — `validation.go`: the deprecated aliases `ForbiddenChar`, `CrontabForbiddenChars` and `ValidateNoForbiddenChars` are removed; `ForbiddenCharacter`, `CrontabForbiddenCharacters` and `ValidateNoForbiddenCharacters` remain.
- **Breaking** — `errors.go`: `ErrSharedRunnerCommandFlags`, deprecated and answered by nothing since the runner accepts a command answering the same flag instances.

### Fixed

- runner: a sibling failure's cause chain renders each pointer link once, breadth-first, as the framework's chain does, where a chain reaching itself repeated its links up to the bound of eight.
- runner: a scheduled command's error whose `Unwrap` chain reaches itself no longer holds the dispatch worker that classifies it, nor the command that renders it.

