# Changelog

All notable changes to `precision-soft/melody/integrations/cron/v2` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `runner_command.go`, `schedule_matcher.go` — `NewRunnerCommand(configuration, dialect, commands...)` adds an in-process scheduler command `melody:cron:run`: it parses the same five-field `Schedule` the generator renders and invokes each registered command when its schedule is due, so a single-binary deployment with no external crontab or kubernetes drives its scheduled work from the one `Configuration` that already feeds the generator. Each command is dispatched through the cli library with its declared flags — unset flags read their declared defaults, `CommandContext.Writer`/`ErrWriter` are usable and `Args()` is initialized, the same surface a command sees under the cli entry point — on a fresh scope and a cancellable child context; a failing or panicking job is logged (a scope-close failure joined onto its error) and the loop continues. Entries sharing a minute run concurrently, each in its own goroutine — like crontab starting an independent process per entry — so one slow job
  delays neither its minute-mates nor the scheduler loop; an entry that outlives its own interval therefore overlaps itself (wrap the command in a locker-backed exclusivity wrapper to serialize successive runs), and on shutdown the loop stops ticking and waits for in-flight jobs, whose contexts derive from the runtime context. The loop evaluates the minute its timer was armed for and reconciles wall-clock jumps with the vixie virtual-time algorithm: after a forward jump of less than three hours (daylight-saving spring-forward, suspend, ntp step) fixed-time entries — those pinning both minute and hour — catch up exactly once for every skipped wall minute while wildcard entries resume at the current minute; after a backward jump of less than three hours (daylight-saving fall-back) wildcard entries keep running every minute while fixed-time entries are suppressed for the repeated span; a jump of three hours or more re-anchors to the current minute without catch-up and logs the reset. The
  day-of-month / day-of-week combination follows a configurable dialect, `ModuleConfig.RunnerDialect` (second argument of `NewRunnerCommand`): the default `RunnerDialectCrontab` — also the zero value — follows vixie crond, whose day-star flag reads just the field's first character, so a star-based day field (the plain or the stepped wildcard, e.g. `*/2`) counts as unrestricted and the day fields combine with *and*, firing `0 0 */2 * 1` only on odd-numbered Mondays; `RunnerDialectKubernetes` follows the robfig scheduler behind the k8s template, where only the shapes the robfig star bit survives — the plain `*` and the unit-stepped wildcard, alone or inside a list — are unrestricted, so a stepped wildcard day field with a step above one stays restricted and combines with *or*, firing on any odd day or any Monday; the kubernetes dialect also bounds day of week at 6, as robfig does, so the Sunday alias `7` fails at construction instead of rendering a CronJob manifest the cluster rejects.
  Two genuinely restricted day fields combine with *or* in both dialects — the dialect changes only how a star-based field is classified — and the divergence on the star-based shapes is inherent to the two target schedulers, so pick the dialect of the manifests the same `Configuration`
  generates. A value naming no known dialect panics at construction with the new `ErrUnknownRunnerDialect`. Construction panics with `ErrUnsupportedRunnerEntry` when the shared `Configuration` contains an entry the runner cannot honor — a custom argv (`EntryConfig.Command`) or more than one instance — with the new `ErrDuplicateRunnerCommand` when two supplied commands share one name, since keeping the later one would silently drop the other (an exclusivity wrapper over its wrapped command, most likely), and with the new `ErrSharedRunnerCommandFlags` when a command returns the same flag instances on every `Flags()` call, since the cli library writes parse state into the instances and the runner's overlapping invocations would race on them. An entry that names a system user (`EntryConfig.User`) stays runnable — in-process every job runs as the process user — and `Run` logs one warning naming the affected commands, so the one `Configuration` keeps driving both the generated manifests and
  the runner. The schedule parser rejects a field carrying whitespace of any kind — leading, trailing or embedded, a no-break space or a vertical tab included. Embedded whitespace is the correctness case: crond splits the line on it and then refuses the whole crontab with a parse error, dropping every entry in the file (measured against vixie crond, which fails alike on an ascii space, a vertical tab and a no-break space); leading and trailing whitespace crond tolerates, and is refused only because the generator always has and the two halves are one rule. The parser also rejects a sign-prefixed number (`"+5"`, which `strconv` accepts and crond does not) and a step on a single value (`"5/15"`) — the one shape no two target schedulers agree on: vixie crond rejects it as a bad field and refuses the entire crontab, taking every other entry in the file down with it, busybox crond accepts it, and the robfig scheduler behind the k8s template reads it as the range from that value up — so
  neither half picks a meaning. The generator's field validation was widened to the same whitespace rule and now rejects the stepped single value too (new `ErrSteppedSingleValue`, naming the unambiguous rewrite: `"5-59/15"`), so the two halves are one rule and a schedule that runs in-process always generates. A step wider than the field is clamped to the field's cardinality rather than rejected, matching crond (whose expansion strides past the high bound on the first hop, so only the range's low value fires) while keeping a step near the integer maximum from overflowing the expansion loop into values the range never allowed. A scheduled command whose error carries an exit code is returned as a plain failure instead of reaching the cli library's default handler, which calls `os.Exit` and would take the whole scheduler down with the one job; the loop's chain anchor and first armed minute derive from one clock read, so startup cannot manufacture a phantom jump that skips the first minute
  for wildcard entries; and a wake that re-arrives at the wall minute the previous wake already dispatched is skipped, since a backward wall step inside the armed window makes the loop arm for that minute a second time and every wildcard entry would otherwise run twice seconds apart (the repeated wall minute of a fall-back is a different case — a whole hour of other minutes runs in between — and still fires). `--once` evaluates every schedule against the current time, runs the due commands and exits. Multi-instance safety is composed in — wrap a command in a distributed-lock exclusivity wrapper, or gate the whole runner behind a leader gate, before listing it (the melody/v3 `lock` package ships both; this major has no `lock` package, so the wrapper is composed in userland). Enable it through `ModuleConfig.RunnerCommands`.

## [v2.3.0] - 2026-07-11 - User-less Crontab Template

### Added

- `template_crontab.go`, `template.go` — new built-in template `crontab-no-user`: the user-less crontab dialect for busybox crond (alpine images) and per-user `crontab` files, which reject the `/etc/cron.d` user column the default `crontab` template emits — previously consumers cut the column with `sed` in the image build. Select it with `--template=crontab-no-user` or the `melody.cron.template` parameter; the user parameter/flag is ignored entirely in this dialect, everything else — schedules, log redirects, heartbeat, validation — is identical. Back-port from `v3`.

### Fixed

- `generate_command.go` — `melody:cron:generate` no longer aborts with `ErrHeartbeatUserMissing` when a heartbeat is configured for the `crontab-no-user` template. That template renders no user column (busybox crond, per-user crontab deployments), so a heartbeat had no user column to attach to and the pre-render check rejected an otherwise valid configuration.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/cron/v2.3.0...HEAD

[v2.3.0]: https://github.com/precision-soft/melody/compare/integrations/cron/v2.2.2...integrations/cron/v2.3.0

[v2.2.2]: https://github.com/precision-soft/melody/compare/integrations/cron/v2.2.1...integrations/cron/v2.2.2

[v2.2.1]: https://github.com/precision-soft/melody/compare/integrations/cron/v2.2.0...integrations/cron/v2.2.1

[v2.2.0]: https://github.com/precision-soft/melody/compare/integrations/cron/v2.1.0...integrations/cron/v2.2.0

[v2.1.0]: https://github.com/precision-soft/melody/compare/integrations/cron/v2.0.0...integrations/cron/v2.1.0

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/cron/v2.0.0
