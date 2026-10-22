# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/pgsql/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The major is created as a complete copy of the third major at `8cc99290`, with its module paths moved to the fourth; the history before this line is in the third major's changelog.

### Changed

- **Breaking** — `timeout_config.go`: `NewTimeoutConfig` takes the connect timeout and the read and write deadlines, the mysql signature, and `NewTimeoutConfigWithDeadlines`, the name the third major gave that form beside the one-duration constructor, is removed.
