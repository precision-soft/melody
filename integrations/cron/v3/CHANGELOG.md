# Changelog

All notable changes to `precision-soft/melody/integrations/cron/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.3.0] - 2026-06-25 - Kubernetes CronJob Template

### Added

- `v3/template_k8s.go` — built-in `k8s` template (`cron.TemplateNameK8s == "k8s"`, registered automatically by `BuiltinTemplates()`) that renders the same `cron.Configuration` as a multi-document YAML stream of `batch/v1` `CronJob` manifests (one per scheduled command, `---`-separated), selectable with `--template=k8s`. Each manifest derives `metadata.name` from the command name sanitized to an RFC 1123 DNS label (lowercased; non-alphanumeric runs collapse to `-`; trimmed; capped at 52 octets), sets `spec.schedule` to `Schedule.Expression()`, and runs the container image's entrypoint with `args: [<command-name>, …]` so the application enters CLI mode from those arguments (a per-command `EntryConfig.Command` override replaces the entrypoint via the k8s `command:` field instead). A command with `EntryConfig.Instances > 1` emits one `CronJob` per instance, each with a `-<index>` suffix on `metadata.name` (the sanitized base is shortened so the suffixed name stays within the 52-octet cap).
  `restartPolicy` defaults to `OnFailure` and is restricted to `OnFailure` or `Never` (`cron.ErrK8sInvalidRestartPolicy`) since a CronJob pod template rejects any other value. Two commands that sanitize to the same resource name are rejected (`cron.ErrK8sDuplicateName`) rather than emitting CronJobs that would silently overwrite each other on `kubectl apply` — and because the namespace is one global option, the collision is detected across every destination file in a single run, not just within one manifest stream. The same per-field schedule validation as the crontab template is applied (embedded whitespace, `%`, CR and LF are rejected). Line terminators are rejected outright in the other user-supplied values with an actionable error; every scalar is emitted double-quoted (with any remaining C0/C1 control or DEL byte escaped as `\xNN`, and the Unicode line/paragraph separators `U+2028`/`U+2029` escaped as `\uNNNN`) so colons, spaces, and cron wildcards survive intact while a stray
  non-printable byte can never break the document.
- `v3/generate_command.go` — `--image` / `--namespace` / `--restart-policy` flags, cascading through the `melody.cron.k8s.image` / `melody.cron.k8s.namespace` / `melody.cron.k8s.restart_policy` container parameters (not registered by `RegisterDefaultParameters` — the crontab template needs none of them). The k8s template requires a non-empty image and fails generation otherwise. The heartbeat options remain crontab-only and are ignored by the k8s template; selecting `--template=k8s` with heartbeat options configured now prints a warning so the dropped liveness entry is not silent. Because the k8s template logs to container stdout and never reads a per-entry log path, a `--template=k8s` run no longer inherits the crontab-only requirements: it does not demand a `--logs-dir` (it never auto-derives or auto-enables a heartbeat either) and a heartbeat left configured does not force a `--user` — the heartbeat is simply ignored with the warning above.

### Fixed

- `v3/validation.go` — the schedule-field whitespace error message is now template-agnostic ("schedule fields must be single tokens" rather than "crontab fields ..."), since `validateScheduleFields` is shared by both the crontab and the k8s template.
- `v3/generate_command.go` — `--template=k8s` no longer hard-fails on a `--heartbeat-destination` value, which the k8s template explicitly ignores. The heartbeat-destination resolution (which rejects a value that matches none of the written destinations with `cron.ErrHeartbeatDestinationUnmatched`) was run unconditionally, so a k8s run that passed `--heartbeat-destination` could fail on the very setting the preceding warning declared ignored — even though the k8s template never emits a heartbeat CronJob. The requested heartbeat destinations are now dropped for the k8s template, so the generation succeeds with no behavioural change to the rendered manifests.

## [v3.2.0] - 2026-06-16 - Plug-and-Play Command Registration

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/cron/v3.3.0...HEAD

[v3.3.0]: https://github.com/precision-soft/melody/compare/integrations/cron/v3.2.0...integrations/cron/v3.3.0

[v3.2.0]: https://github.com/precision-soft/melody/compare/integrations/cron/v3.1.0...integrations/cron/v3.2.0

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/cron/v3.0.0...integrations/cron/v3.1.0

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/cron/v3.0.0
