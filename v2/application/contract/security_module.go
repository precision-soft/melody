package contract

import (
    securityconfig "github.com/precision-soft/melody/v2/security/config"
)

/* SecurityModule is the hook through which a module contributes firewalls, providers and access rules to the one security configuration the application compiles, in registration order; the compiled result exists only after every module ran. */
type SecurityModule interface {
    Module

    RegisterSecurity(builder *securityconfig.Builder)
}
