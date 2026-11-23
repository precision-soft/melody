package config

import (
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/security"
)

/* BootWarning is one hazard the compiled declaration admits; the application writes each into the configured journal once, at the boot */
type BootWarning = internal.BootWarning

func compileBootWarnings(compiled *security.CompiledConfiguration, global GlobalConfig, firewalls []firewallDeclaration) []BootWarning {
    warnings := make([]BootWarning, 0)

    if nil == compiled {
        return warnings
    }

    warnings = append(warnings, firewallDoorReachWarnings(compiled.Firewalls())...)
    warnings = append(warnings, csrfDisabledLoginWarnings(global, firewalls)...)

    return warnings
}

/* csrfDisabledLoginWarnings names every firewall that signs users in and checks none of their requests for forgery */
func csrfDisabledLoginWarnings(global GlobalConfig, firewalls []firewallDeclaration) []BootWarning {
    warnings := make([]BootWarning, 0)

    for _, firewall := range firewalls {
        if "" == firewall.config.Login.Path || security.CsrfModeDisabled != effectiveCsrf(global, firewall).Mode {
            continue
        }

        declaredBy := "global"
        if "" != firewall.config.Csrf.Mode {
            declaredBy = "firewall"
        }

        warnings = append(warnings, BootWarning{
            Name:    internal.BootWarningNameCsrfDisabledOnLoginFirewall,
            Message: "a firewall that signs users in checks no request for cross-site forgery",
            Context: loggingcontract.Context{
                "firewall":   firewall.name,
                "declaredBy": declaredBy,
            },
        })
    }

    return warnings
}
