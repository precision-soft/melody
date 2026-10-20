# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The major is created as a complete copy of the third major at `8cc99290`, with its module paths moved to the fourth; the history before this line is in the third major's changelog.

### Fixed

- `audit/async_storage.go` — carried from the third major (v3.6.1 and after): `AsyncStorage.CloseWithContext` reads a deadline reached during one of its two stretches as the budget running out, never as its caller cancelling the close — abandoned while it waited for a save that ignored its cancellation, the budget spent during the drain or with the drain's timer — and every answer the deadline ends carries `context.DeadlineExceeded`; when the deadline, not the second grace, ended the wait, the answer says the deadline cut the grace short and carries `waited` beside the planned `grace`.
