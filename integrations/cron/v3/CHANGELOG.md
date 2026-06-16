# Changelog

All notable changes to `precision-soft/melody/integrations/cron/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.2.0] - 2026-06-15 - Plug-and-Play Command Registration

### Added

- `v3/command.go` — `Commands(configuration)` returns the `melody:cron:generate` command as a `[]cli/contract.Command`, so userland registers the integration's built-in command in one call.
- `v3/module.go` — `cron.NewModule(ModuleConfig{Configuration | ConfigurationFactory, WithDefaultParameters})` self-registering application module that registers the crontab-generation command and, opt-in, the default parameters, replacing hand-written `Commands`/`RegisterDefaultParameters` wiring. `ConfigurationFactory func(kernel) *Configuration` is evaluated at command-registration time (when the kernel/container exists), for the common case where the `Configuration` depends on resolved parameters or the manager registry; it takes precedence over the eager `Configuration` when both are set.

### Fixed

- `v3/validation.go` — the crontab schedule fields (`Minute`/`Hour`/`DayOfMonth`/`Month`/`DayOfWeek`) are now validated against `CrontabForbiddenChars`, like every other token emitted into a crontab line, so a `%` is rejected at the source instead of being written verbatim. `%` is crontab's line-continuation character (translated to a newline before the shell sees it); the schedule fields previously checked only for whitespace, so a `%` slipped through and corrupted the generated entry.
- `v3/validation.go` — the crontab user field (`Entry.User` and the heartbeat user) is now validated against `CrontabForbiddenChars` too, closing the sibling gap to the schedule-field fix above. `validateUserField` checked only for whitespace, so a `%` in a user value (from the `--user` flag, the `melody.cron.user` parameter, or `EntryConfig.User`) reached the generated crontab verbatim — and because the user is written into the same line position as the schedule, crond's `%`-to-newline translation split the entry into a malformed line plus a stray trailing line. The user value now runs through `ValidateNoForbiddenChars`, rejecting `%` at the source.

## [v3.1.0] - 2026-05-19 - Auto-Derive Heartbeat Path and Auto-Create Logs Directory

Identical to the corresponding v1 release except: module path is `github.com/precision-soft/melody/integrations/cron/v3`; dependency pinned to `github.com/precision-soft/melody/v3`. See the [v1 changelog](../CHANGELOG.md#v110---2026-05-19---auto-derive-heartbeat-path-and-auto-create-logs-directory) for the full change list.

## [v3.0.0] - 2026-05-16 - Initial Release — Cron Integration

Identical to the corresponding v1 release except: module path is `github.com/precision-soft/melody/integrations/cron/v3`; dependency pinned to `github.com/precision-soft/melody/v3`. See the [v1 changelog](../CHANGELOG.md#v100---2026-05-16---initial-release--cron-integration) for the full change list.

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/cron/v3.2.0...HEAD

[v3.2.0]: https://github.com/precision-soft/melody/compare/integrations/cron/v3.1.0...integrations/cron/v3.2.0

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/cron/v3.0.0...integrations/cron/v3.1.0

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/cron/v3.0.0
