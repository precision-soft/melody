package contract

import (
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* BootWarningContextKey is the context key every boot warning carries its code under, the framework's and a module's alike, so a journal filter selects them by one key */
const BootWarningContextKey = internal.BootWarningContextKey

/* BootWarning is a hazard a module's configuration admits, written once into the configured journal beside the framework's own boot warnings. Name is the stable dotted code the module declared; Context names the configuration and never a value that may be a secret. */
type BootWarning struct {
    Name    string
    Message string
    Context loggingcontract.Context
}

/* BootWarningConfig is how a declared boot warning code is written; it holds no field yet, and every zero value is the default. */
type BootWarningConfig struct{}

/* BootWarningRegistrar is handed to a module before the configuration resolves. A declared code is one MELODY_BOOT_WARNINGS_IGNORED may list, in the http and the cli process alike; a warning added under it is written with the framework's boot warnings and honours that key. */
type BootWarningRegistrar interface {
    DeclareBootWarning(name string, config BootWarningConfig)

    AddBootWarning(warning BootWarning)
}

/* BootWarningModule declares the boot warning codes a module writes and adds the warnings its configuration calls for. A name held by the framework or by another module, a warning under a name the module did not declare, and an empty message fail the boot. */
type BootWarningModule interface {
    Module
    RegisterBootWarnings(registrar BootWarningRegistrar)
}
