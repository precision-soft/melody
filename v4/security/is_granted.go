package security

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

func IsGranted(runtimeInstance containercontract.Runtime, role string) bool {
    if nil == runtimeInstance {
        return false
    }

    securityContext, exists := SecurityContextFromRuntime(runtimeInstance)
    if false == exists {
        return false
    }

    return securityContext.IsGranted(role)
}
