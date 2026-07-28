package handler

import (
    nethttp "net/http"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* RedisTokenRevocationHandler shows that a token can be taken away before it expires.

A signed token is valid until its own expiry and no server holds an opinion about it, which is why revocation needs a store: the token is looked up on every use, and deleting the entry ends the session at once rather than whenever the clock catches up. The response reports the lookup on both sides of the revocation, because only the pair says anything — a store that always answered "not found" would satisfy the second half alone. */
func RedisTokenRevocationHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        store := melodyrueidis.TokenStoreMustFromResolver(runtimeInstance.Container())

        token := "example-revocable-token"
        store.Put(token, melodysecuritycontract.Claims{
            UserIdentifier: "revocation-probe-user",
            Roles:          []string{"ROLE_USER"},
        })

        claims, foundBeforeRevoke, lookupErr := store.Lookup(runtimeInstance, token)
        if nil != lookupErr {
            return nil, lookupErr
        }

        store.Delete(token)

        _, foundAfterRevoke, revokeLookupErr := store.Lookup(runtimeInstance, token)
        if nil != revokeLookupErr {
            return nil, revokeLookupErr
        }

        return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{
            "user":              claims.UserIdentifier,
            "foundBeforeRevoke": foundBeforeRevoke,
            "foundAfterRevoke":  foundAfterRevoke,
        })
    }
}
