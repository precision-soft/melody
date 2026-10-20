# Changelog

All notable changes to `precision-soft/melody/integrations/amqp` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The major is created as a complete copy of the third major at `8cc99290`, with its module paths moved to the fourth; the history before this line is in the third major's changelog.

### Security

- `go.mod` — carried from the third major (v3.3.1): `github.com/rabbitmq/amqp091-go` is required at v1.14.0, the fix for [GO-2026-6700](https://pkg.go.dev/vuln/GO-2026-6700), reachable through `Provider.Open`, which dials with `amqp091.DialConfig`.
