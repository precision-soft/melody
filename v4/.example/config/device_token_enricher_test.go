package config

import (
    "errors"
    "slices"
    "testing"

    "github.com/precision-soft/melody/v4/.example/entity"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

func TestDeviceAccountEnricher_HoldsTheTokenToItsAccount(t *testing.T) {
    directory := map[string]*entity.User{
        "user-2": entity.NewUser("user-2", "editor", "hash", []string{entity.RoleUser}),
    }
    enricher := newDeviceAccountEnricher(func(runtimeInstance containercontract.Runtime, userIdentifier string) (*entity.User, bool, error) {
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

    for userIdentifier, infrastructureFailure := range map[string]bool{"user-9": false, "user-broken": true} {
        _, refusal := enricher.Enrich(nil, melodysecuritycontract.Claims{UserIdentifier: userIdentifier, Roles: []string{entity.RoleAdmin}})
        if nil == refusal {
            t.Fatalf("%s: expected the token refused", userIdentifier)
        }

        if infrastructureFailure != exception.IsInfrastructureFailure(refusal) {
            t.Fatalf("%s: expected the infrastructure mark to be %v, got %v", userIdentifier, infrastructureFailure, refusal)
        }
    }
}
