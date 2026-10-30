# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major up to v3.6.0; that history is in the third major's changelog, and the third-major fixes released after it are listed below as carried.
- `diagnostics.go`: the logger the registry hands its providers forwards the wrapped logger's cause chain depth, as it forwards its threshold, so `logging.LogError` through it writes the chain the application's logger was configured for.
- `split.go`: `SplitterConfig`, the one struct a splitter is built from, `ReplicaNames` and `ReplicaRetryInterval`, 5s when left out, at least 1ms, unlimited refused.
- `errors.go`: `ErrConnectionHostIsRequired` and `ErrConnectionDatabaseIsRequired`, the causes of the registry's refusal of an empty `Params.Host` or `Params.Database`.
- `audit/config.go`, `audit/recorder.go`, `audit/async_storage.go`: `RegistryConfig`, `RecorderConfig` and `AsyncStorageConfig`, the one struct each is built from; `AsyncStorageConfig.CloseGrace` gives the close's grace a door, 5s when left out as the constant was, at least 1ms, unlimited refused.

### Changed

- **Breaking** — `manager_registry.go`: `NewManagerRegistry` takes the context its lazy opens run under, a nil one read as `context.Background()`, the logger and the definitions as a `[]ProviderDefinition`; `NewManagerRegistryWithContext` is removed.
- **Breaking** — `manager_registry.go`: the registry refuses a definition whose `Params.Host` or `Params.Database` is empty, by its position and name, naming the field `ProviderDefinition.Params.Host` or `ProviderDefinition.Params.Database`, where an empty host was refused at the first open and an empty database dialled on mysql.
- **Breaking** — `connection_parameters.go`: the deprecated alias `ConnectionParams` is removed; `ConnectionParameters` is the one shape.
- **Breaking** — `split.go`: `NewReadWriteSplitter` takes the registry, the primary's name and a `SplitterConfig`, a `ReplicaRetryInterval` outside its rule panicking naming the field where a zero interval took the default; `NewReadWriteSplitterWithOptions`, `ReadWriteSplitterOption` and `WithReplicaRetryInterval` are removed.
- **Breaking** — `audit/config.go`: `NewRegistry` takes a `RegistryConfig{DefaultTable, GlobalIgnoredFields}`, a table that is not a plain SQL identifier refused naming `RegistryConfig.DefaultTable`.
- **Breaking** — `audit/recorder.go`: `NewRecorder` takes the storage and a `RecorderConfig{Registry, OwnsStorage, Logger}`, a typed-nil logger refused naming the field, the logger set once; the `(database, table)` door, `NewRecorderWithStorage`, `NewRecorderOwningStorage` and `Recorder.WithLogger` are removed.
- **Breaking** — `audit/async_storage.go`: `NewAsyncStorage` takes the delegate and an `AsyncStorageConfig{BufferSize, Logger, CloseGrace}`, `BufferSize` 1024 when left out, at least 1, unlimited refused, where a non-positive size took 1024, the logger set once, a typed nil refused, a field outside its rule panicking naming it; `AsyncStorage.WithLogger` is removed.
- **Breaking** — `audit/storage.go`: the `Storage` contract's `Save` takes the entries as a `[]Entry`, where it was variadic; a nil or empty slice is a no-op on `BunStorage`, `FileStorage` and `AsyncStorage`.
- `audit/async_storage.go` and `audit/recorder.go`: the log records are built through `exception.LogContext(err, exception.LogContextConfig{...})`, the fourth major's form, which takes the extra context in a struct; nothing of this module's own surface changes.

### Fixed

- `audit/async_storage.go` — carried from the third major (v3.6.1 and v3.6.2): `AsyncStorage.CloseWithContext` reads a deadline reached during one of its two stretches as the budget running out, never as its caller cancelling the close — abandoned while it waited for a save that ignored its cancellation, the budget spent during the drain or with the drain's timer — and every answer the deadline ends carries `context.DeadlineExceeded`; when the deadline, not the second grace, ended the wait, the answer says the deadline cut the grace short and carries `waited` beside the planned `grace`.
- `audit/async_storage.go` — carried from the third major (v3.6.2): `AsyncStorage.CloseWithContext` reached with a context that had already ended carries that context's error as the cause of its answer: a deadline passed before the close began is read off the answer as `context.DeadlineExceeded`, as a deadline reached during a stretch is, where the answer carried no cause; and a context its caller cancelled before the close began is answered as the caller's withdrawal with `context.Canceled` as its cause, where it read that the budget was already spent.
- `audit/async_storage.go` — carried from the third major (v3.6.3): the close that abandons a save which ignored its cancellation carries the deadline as its cause and says the deadline cut the second drain grace short also when the stretch's timer and the deadline were ready in the same instant and the wait answered on the timer, where that pick of the race answered with no cause and read "after a second drain grace"; and a close its caller cancels once it has begun, with no grace left to observe a reaction in, is answered as the caller's withdrawal after the close began, where it read that the close was cancelled before it began, the context being read at entry once, before anything waits.
