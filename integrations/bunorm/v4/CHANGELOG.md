# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The major is created as a complete copy of the third major at `8cc99290`, with its module paths moved to the fourth; the history before this line is in the third major's changelog.

### Fixed

- `audit/async_storage.go` — carried from the third major (v3.6.1 and v3.6.2): `AsyncStorage.CloseWithContext` reads a deadline reached during one of its two stretches as the budget running out, never as its caller cancelling the close — abandoned while it waited for a save that ignored its cancellation, the budget spent during the drain or with the drain's timer — and every answer the deadline ends carries `context.DeadlineExceeded`; when the deadline, not the second grace, ended the wait, the answer says the deadline cut the grace short and carries `waited` beside the planned `grace`.
- `audit/async_storage.go` — carried from the third major (v3.6.2): `AsyncStorage.CloseWithContext` reached with a context that had already ended carries that context's error as the cause of its answer: a deadline passed before the close began is read off the answer as `context.DeadlineExceeded`, as a deadline reached during a stretch is, where the answer carried no cause; and a context its caller cancelled before the close began is answered as the caller's withdrawal with `context.Canceled` as its cause, where it read that the budget was already spent.
- `audit/async_storage.go` — carried from the third major (v3.6.3): the close that abandons a save which ignored its cancellation carries the deadline as its cause and says the deadline cut the second drain grace short also when the stretch's timer and the deadline were ready in the same instant and the wait answered on the timer, where that pick of the race answered with no cause and read "after a second drain grace"; and a close its caller cancels once it has begun, with no grace left to observe a reaction in, is answered as the caller's withdrawal after the close began, where it read that the close was cancelled before it began, the context being read at entry once, before anything waits.
