package application

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    securityconfig "github.com/precision-soft/melody/v3/security/config"
)

type SecurityModule interface {
    applicationcontract.Module
    RegisterSecurity(builder *securityconfig.Builder)
}
