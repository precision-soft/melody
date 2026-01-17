package security

import (
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func IsGranted(runtimeInstance runtimecontract.Runtime, role string) bool {
	if nil == runtimeInstance {
		return false
	}

	securityContext, exists := SecurityContextFromRuntime(runtimeInstance)
	if false == exists {
		return false
	}

	return securityContext.IsGranted(role)
}
