package security

import (
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* TokenFromRuntime answers the token the security listener published for this
request, and false when there is none to read: no security context on the scope,
or a context carrying no token. The eight readers of it in this application each
answer absence their own way -- an unauthorized response, the system actor, a
closed door -- which is why the shared half stops here and hands back a boolean
rather than a decision.

The nil branch is defence in depth, and the measurement that says so belongs
beside it: the framework's resolution listener is the only producer of a security
context, and each of its three exits publishes a live token, normalising a typed
nil from an application token source before it gets there. It stays because
SecurityContext and NewSecurityContext are exported, so an application can build
one with no token and put it on the scope itself. It is a plain nil comparison,
so a TYPED nil of an application's own token type would pass it -- the
framework's IsNilInterface is internal and this module cannot reach it. */
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
