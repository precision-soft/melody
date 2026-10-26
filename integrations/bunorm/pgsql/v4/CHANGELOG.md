# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/pgsql/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major up to v3.3.0; that history is in the third major's changelog.

### Changed

- **Breaking** — `timeout_config.go`: `NewTimeoutConfig` takes the connect timeout and the read and write deadlines, the mysql signature, and `NewTimeoutConfigWithDeadlines`, the name the third major gave that form beside the one-duration constructor, is removed.
- **Breaking** — `service_resolver.go`: `ServiceRegistrar.RegisterService` takes the framework's `RegisterOptions` struct last, the fourth major's registration form, where it took variadic `RegisterOption` functions; the application satisfies it as before.
