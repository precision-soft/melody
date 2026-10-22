# Changelog

All notable changes to `precision-soft/melody/integrations/opentelemetry` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The major is created as a complete copy of the third major at `8cc99290`, with its module paths moved to the fourth; the history before this line is in the third major's changelog.

### Security

- `golang.org/x/net`, which the otlp exporters and grpc require, is raised to `v0.60.0`, the version that fixes GO-2026-6603, GO-2026-6611, GO-2026-6612 and GO-2026-6617, and the module's go directive to `1.26.0`, which that version requires; the third major keeps `v0.57.0` and the go 1.25 floor.
