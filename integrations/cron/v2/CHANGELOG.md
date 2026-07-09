# Changelog

All notable changes to `precision-soft/melody/integrations/cron/v2` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `template_crontab.go`, `template.go` — new built-in template `crontab-no-user`: the user-less crontab dialect for busybox crond (alpine images) and per-user `crontab` files, which reject the `/etc/cron.d` user column the default `crontab` template emits — previously consumers cut the column with `sed` in the image build. Select it with `--template=crontab-no-user` or the `melody.cron.template` parameter; the user parameter/flag is ignored entirely in this dialect, everything else — schedules, log redirects, heartbeat, validation — is identical. Back-port from `v3`.

## [v2.2.2] - 2026-07-06 - Standalone Module Resolution Fix

### Fixed

- `go.mod` — the module pinned `melody/v2 v2.0.0` while importing the `cli/contract.StringSliceFlag` symbol, which only exists from `v2.7.0`, so outside the repository workspace (`GOWORK=off`, or any consumer cloning just this module) the module did not resolve. The pin is raised to `v2.7.0` — the lowest framework version that provides every imported package — and the module-local `go.sum` is now complete for standalone builds.

## [v2.2.1] - 2026-06-25 - Forbid Control-Character Injection in User and Schedule Fields

### Fixed

- Identical to the corresponding v1 entry (`validation.go` — reject `CrontabForbiddenChars` in the user and schedule fields). See the [v1 changelog](../CHANGELOG.md#v121---2026-06-25---forbid-control-character-injection-in-user-and-schedule-fields) for the full description.

## [v2.2.0] - 2026-06-16 - Plug-and-Play Module Registration

Identical to the corresponding v1 release except: module path is `github.com/precision-soft/melody/integrations/cron/v2`; dependency pinned to `github.com/precision-soft/melody/v2`. See the [v1 changelog](../CHANGELOG.md#v120---2026-06-15---plug-and-play-module-registration) for the full change list.

## [v2.1.0] - 2026-05-19 - Auto-Derive Heartbeat Path and Auto-Create Logs Directory

Identical to the corresponding v1 release except: module path is `github.com/precision-soft/melody/integrations/cron/v2`; dependency pinned to `github.com/precision-soft/melody/v2`. See the [v1 changelog](../CHANGELOG.md#v110---2026-05-19---auto-derive-heartbeat-path-and-auto-create-logs-directory) for the full change list.

## [v2.0.0] - 2026-05-16 - Initial Release — Cron Integration

Identical to the corresponding v1 release except: module path is `github.com/precision-soft/melody/integrations/cron/v2`; dependency pinned to `github.com/precision-soft/melody/v2`. See the [v1 changelog](../CHANGELOG.md#v100---2026-05-16---initial-release--cron-integration) for the full change list.

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/cron/v2.2.2...HEAD

[v2.2.2]: https://github.com/precision-soft/melody/compare/integrations/cron/v2.2.1...integrations/cron/v2.2.2

[v2.2.1]: https://github.com/precision-soft/melody/compare/integrations/cron/v2.2.0...integrations/cron/v2.2.1

[v2.2.0]: https://github.com/precision-soft/melody/compare/integrations/cron/v2.1.0...integrations/cron/v2.2.0

[v2.1.0]: https://github.com/precision-soft/melody/compare/integrations/cron/v2.0.0...integrations/cron/v2.1.0

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/cron/v2.0.0
