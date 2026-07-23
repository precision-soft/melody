package container

import (
    "testing"

    containercontract "github.com/precision-soft/melody/container/contract"
    alpha "github.com/precision-soft/melody/container/internal/collisionalpha/contract"
    beta "github.com/precision-soft/melody/container/internal/collisionbeta/contract"
)

/* @info two distinct types from same-named packages share a String() ("contract.Bus"); a nested by-type resolution of one from the other's provider must resolve and close cleanly, not read as a self-cycle on the aliased key */
func TestResolution_SameStringTypesFromDifferentPackagesDoNotAlias(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegister(serviceContainer, "bus.beta", func(resolver containercontract.Resolver) (*beta.Bus, error) {
        return &beta.Bus{Region: "beta"}, nil
    }, WithTypeRegistration(true))

    MustRegister(serviceContainer, "bus.alpha", func(resolver containercontract.Resolver) (*alpha.Bus, error) {
        /* the alpha provider resolves the beta bus by type: both nodes key on "contract.Bus" through String(), so a shared key would flag a false cycle here */
        betaBus, betaErr := FromResolverByType[*beta.Bus](resolver)
        if nil != betaErr {
            return nil, betaErr
        }

        return &alpha.Bus{Region: "alpha:" + betaBus.Region}, nil
    }, WithTypeRegistration(true))

    alphaBus, getErr := FromResolverByType[*alpha.Bus](serviceContainer)
    if nil != getErr {
        t.Fatalf("expected the alpha bus to resolve through the beta bus, got %v", getErr)
    }

    if "alpha:beta" != alphaBus.Region {
        t.Fatalf("expected the nested by-type resolution to compose, got %q", alphaBus.Region)
    }

    betaBus, betaGetErr := FromResolverByType[*beta.Bus](serviceContainer)
    if nil != betaGetErr {
        t.Fatalf("expected the beta bus to resolve, got %v", betaGetErr)
    }

    if "beta" != betaBus.Region {
        t.Fatalf("expected the beta bus to keep its own value, got %q", betaBus.Region)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("expected the container with same-string types to close cleanly, got %v", closeErr)
    }
}

/* @info both same-string types can now be type-registered under their auto-derived names: the name is import-path-qualified, so "contract.Bus" from two packages no longer collides at registration */
func TestRegisterType_SameStringTypesFromDifferentPackagesGetDistinctNames(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*alpha.Bus, error) {
        return &alpha.Bus{Region: "alpha"}, nil
    })

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*beta.Bus, error) {
        return &beta.Bus{Region: "beta"}, nil
    })

    alphaBus, alphaErr := FromResolverByType[*alpha.Bus](serviceContainer)
    if nil != alphaErr {
        t.Fatalf("expected the alpha bus to resolve, got %v", alphaErr)
    }

    betaBus, betaErr := FromResolverByType[*beta.Bus](serviceContainer)
    if nil != betaErr {
        t.Fatalf("expected the beta bus to resolve, got %v", betaErr)
    }

    if "alpha" != alphaBus.Region || "beta" != betaBus.Region {
        t.Fatalf("expected each auto-named registration to keep its own value, got %q and %q", alphaBus.Region, betaBus.Region)
    }
}
