package contract

import (
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* BootWarning is a hazard a module's configuration admits, written once into the configured journal beside the framework's own boot warnings. Name is the stable dotted code the module declared; Context names the configuration and never a value that may be a secret. */
type BootWarning struct {
    Name    string
    Message string
    Context loggingcontract.Context
}

/* BootWarningConfig is how a declared boot warning code is written; every zero value is the default. */
type BootWarningConfig struct {
    /* WrittenInCli declares the code one a cli process writes, so MELODY_CLI_BOOT_WARNINGS_IGNORED may list it; false refuses the code in that list as one only an http process writes. */
    WrittenInCli bool
}

/* BootWarningRegistrar is handed to a module before the configuration resolves. A declared code is one a cli process may list in MELODY_CLI_BOOT_WARNINGS_IGNORED when its config's WrittenInCli is true; a warning added under it is written with the framework's boot warnings and honours the cli keys. */
type BootWarningRegistrar interface {
    DeclareBootWarning(name string, config BootWarningConfig)

    AddBootWarning(warning BootWarning)
}

/* BootWarningModule declares the boot warning codes a module writes and adds the warnings its configuration calls for. A name held by the framework or by another module, a warning under a name the module did not declare, and an empty message fail the boot. */
type BootWarningModule interface {
    Module
    RegisterBootWarnings(registrar BootWarningRegistrar)
}
