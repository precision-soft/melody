package application

import (
    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    securityconfig "github.com/precision-soft/melody/v4/security/config"
)

type SecurityModule interface {
    applicationcontract.Module
    RegisterSecurity(builder *securityconfig.Builder)
}
