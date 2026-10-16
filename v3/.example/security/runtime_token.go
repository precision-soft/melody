package security

import (
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* TokenFromRuntime answers the token the security listener published for this request, and false when there is none: no security context on the scope, or one carrying no token. Its readers each answer absence their own way, so it answers a boolean rather than a decision. The nil check is defence in depth for a context an application builds itself; it is a plain nil comparison, so a typed nil of the application's own token type passes it. */
func TokenFromRuntime(runtimeInstance melodyruntimecontract.Runtime) (melodysecuritycontract.Token, bool) {
    securityContext, exists := melodysecurity.SecurityContextFromRuntime(runtimeInstance)
    if false == exists {
        return nil, false
    }

    token := securityContext.Token()
    if nil == token {
        return nil, false
    }

    return token, true
}
