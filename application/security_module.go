package application

import (
	applicationcontract "github.com/precision-soft/melody/application/contract"
	securityconfig "github.com/precision-soft/melody/security/config"
)

type SecurityModule interface {
	applicationcontract.Module
	RegisterSecurity(builder *securityconfig.Builder)
}
