# VERSION

The [`version`](../../version) package exposes Melody's build version.

## Scope

This package provides a single accessor that returns the version string embedded in the binary at build time.

## Responsibilities

- Expose the build version string:
    - [`BuildVersion()`](../../version/version.go)

## Configuration

### Build-time version override

`version.BuildVersion()` returns a package-level value that is intended to be overridden at build time using Go `-ldflags`.

Example:

```bash
go build -ldflags "-X github.com/precision-soft/melody/version.buildVersion=v1.0.0" -o app ./...
```

## Usage

```go
package main

import (
    "fmt"

    "github.com/precision-soft/melody/version"
)

func main() {
    fmt.Println(
        version.BuildVersion(),
    )
}
```

## Userland API

- [`BuildVersion() string`](../../version/version.go)
