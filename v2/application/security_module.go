package application

import (
    applicationcontract "github.com/precision-soft/melody/v2/application/contract"
    securityconfig "github.com/precision-soft/melody/v2/security/config"
)

type SecurityModule interface {
    applicationcontract.Module
    RegisterSecurity(builder *securityconfig.Builder)
}
