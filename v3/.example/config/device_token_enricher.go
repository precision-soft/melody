package config

import (
    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyexceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* deviceAccountLookup reads the account a device token names */
type deviceAccountLookup func(runtimeInstance runtimecontract.Runtime, userIdentifier string) (*entity.User, bool, error)

/* repositoryDeviceAccountLookup reads the account from the repository, never from a cache, as the session does: the token's authority is the account's current state, and a cached copy would keep a deleted or demoted account's authority alive */
func repositoryDeviceAccountLookup(runtimeInstance runtimecontract.Runtime, userIdentifier string) (*entity.User, bool, error) {
    return repository.MustGetUserRepository(runtimeInstance.Container()).FindById(runtimeInstance.Context(), userIdentifier)
}

/* deviceAccountEnricher holds a device token to the account it names: the token is honoured only while that account exists, with the roles the account holds now rather than the ones frozen into the token at issue. A token that outlived its account — issued beside the deletion, or left by a release that failed — names an identifier the next account may receive, and a demoted account's token would keep the roles it lost. A refusal here answers the request as anonymous, as the bearer source answers every refused token. */
type deviceAccountEnricher struct {
    lookup deviceAccountLookup
}

func newDeviceAccountEnricher(lookup deviceAccountLookup) deviceAccountEnricher {
    return deviceAccountEnricher{lookup: lookup}
}

func (instance deviceAccountEnricher) Enrich(
    runtimeInstance runtimecontract.Runtime,
    claims melodysecuritycontract.Claims,
) (melodysecuritycontract.Claims, error) {
    account, found, lookupErr := instance.lookup(runtimeInstance, claims.UserIdentifier)
    if nil != lookupErr {
        return claims, melodyexception.NewError(
            "the account a device token names could not be read",
            melodyexceptioncontract.Context{"userIdentifier": claims.UserIdentifier},
            lookupErr,
        )
    }

    if false == found || nil == account {
        return claims, melodyexception.NewError(
            "the account a device token names no longer exists",
            melodyexceptioncontract.Context{"userIdentifier": claims.UserIdentifier},
            nil,
        )
    }

    claims.Roles = append([]string{}, account.Roles...)

    return claims, nil
}

var _ melodysecuritycontract.TokenEnricher = deviceAccountEnricher{}
