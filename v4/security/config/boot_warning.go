package config

import (
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/security"
)

/* BootWarning is one hazard the compiled declaration admits; the application writes each into the configured journal once, at the boot */
type BootWarning = internal.BootWarning

func compileBootWarnings(compiled *security.CompiledConfiguration) []BootWarning {
    warnings := make([]BootWarning, 0)

    if nil == compiled {
        return warnings
    }

    warnings = append(warnings, firewallDoorReachWarnings(compiled.Firewalls())...)

    return warnings
}
