package contract

import (
    securityconfig "github.com/precision-soft/melody/v2/security/config"
)

/* SecurityModule is the hook through which a module contributes firewalls, providers and access rules to the one security configuration the application compiles. Every module's contribution lands on the same builder, in registration order, and the compiled result exists only after all of them ran. */
type SecurityModule interface {
    Module

    RegisterSecurity(builder *securityconfig.Builder)
}
