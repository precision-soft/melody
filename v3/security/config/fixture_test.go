package config

import (
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

type anonymousTokenSource struct{}

func (instance *anonymousTokenSource) Name() string {
    return "anonymous"
}

func (instance *anonymousTokenSource) Resolve(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
) (securitycontract.Token, error) {
    _ = runtimeInstance
    _ = request

    return security.NewAnonymousToken(), nil
}

var _ securitycontract.TokenSource = (*anonymousTokenSource)(nil)
