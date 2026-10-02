package config

import (
    "errors"
    "slices"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* a device token is honoured only while its account exists, with the roles the account holds now: a token naming an account that is gone is refused, one whose account was demoted carries the account's roles and not the ones frozen at issue, and a directory that cannot answer refuses rather than honours */
func TestDeviceAccountEnricher_HoldsTheTokenToItsAccount(t *testing.T) {
    directory := map[string]*entity.User{
        "user-2": entity.NewUser("user-2", "editor", "hash", []string{entity.RoleUser}),
    }
    enricher := newDeviceAccountEnricher(func(runtimeInstance runtimecontract.Runtime, userIdentifier string) (*entity.User, bool, error) {
        if "user-broken" == userIdentifier {
            return nil, false, errors.New("the directory is down")
        }

        account, found := directory[userIdentifier]

        return account, found, nil
    })

    enriched, enrichErr := enricher.Enrich(nil, melodysecuritycontract.Claims{UserIdentifier: "user-2", Roles: []string{entity.RoleAdmin}})
    if nil != enrichErr || false == slices.Equal([]string{entity.RoleUser}, enriched.Roles) {
        t.Fatalf("expected the account's current roles in place of the token's, got %v (%v)", enriched.Roles, enrichErr)
    }

    /* only the directory that cannot answer is an infrastructure failure, which the bearer source files at Error; an account that is gone is the routine refusal */
    for userIdentifier, infrastructureFailure := range map[string]bool{"user-9": false, "user-broken": true} {
        _, refusal := enricher.Enrich(nil, melodysecuritycontract.Claims{UserIdentifier: userIdentifier, Roles: []string{entity.RoleAdmin}})
        if nil == refusal {
            t.Fatalf("%s: expected the token refused", userIdentifier)
        }

        if infrastructureFailure != melodysecurity.IsInfrastructureFailure(refusal) {
            t.Fatalf("%s: expected the infrastructure mark to be %v, got %v", userIdentifier, infrastructureFailure, refusal)
        }
    }
}
