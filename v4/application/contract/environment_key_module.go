package contract

import (
    configcontract "github.com/precision-soft/melody/v4/config/contract"
)

/* EnvironmentKeyRegistrar is handed to a module first in Boot, before any parameter hook and before the configuration resolves. */
type EnvironmentKeyRegistrar interface {
    DeclareEnvironmentKey(key configcontract.EnvironmentKey)
}

/* EnvironmentKeyModule declares the .env keys a module owns. A key outside the MELODY_ namespace or held by the framework or another declaration, a parameter already registered, a secret key with a default and a default its own rule refuses fail the boot naming the module, the key and the field; after the resolve, a value its rule refuses fails it the same way. The hook declares and must not read. */
type EnvironmentKeyModule interface {
    Module
    RegisterEnvironmentKeys(registrar EnvironmentKeyRegistrar)
}
