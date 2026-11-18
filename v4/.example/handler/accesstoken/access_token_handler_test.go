package accesstoken

import (
    "context"
    "encoding/json"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/.example/repository"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodyconfigcontract "github.com/precision-soft/melody/v4/config/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v4/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
    melodyvalidation "github.com/precision-soft/melody/v4/validation"
)

type emptyEnvironmentSource struct{}

func (instance emptyEnvironmentSource) Load() (map[string]string, error) {
    return map[string]string{}, nil
}

type accessTokenDoorCall struct {
    handler        melodyhttpcontract.Handler
    path           string
    body           string
    userIdentifier string
    tokenStore     melodysecuritycontract.EpochRevocableTokenStore
    handlerErr     *error
}

func callAccessTokenDoor(t *testing.T, call accessTokenDoorCall) int {
    t.Helper()

    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil).WithAccountSeed())
    if nil != repositoryErr {
        t.Fatalf("build the user repository: %v", repositoryErr)
    }

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(containerInstance, repository.ServiceUserRepository, func(resolver melodycontainercontract.Resolver) (repository.UserRepository, error) {
        return userRepository, nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister(containerInstance, melodyrueidis.ServiceTokenStore, func(resolver melodycontainercontract.Resolver) (melodysecuritycontract.EpochRevocableTokenStore, error) {
        return call.tokenStore, nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister(containerInstance, melodylogging.ServiceLogger, func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
        return melodylogging.NewNopLogger(), nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister(containerInstance, melodyclock.ServiceClock, func(resolver melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
        return melodyclock.NewSystemClock(), nil
    }, melodycontainer.RegisterOptions{})

    environment, environmentErr := melodyconfig.NewEnvironment(emptyEnvironmentSource{})
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    melodycontainer.MustRegister(containerInstance, melodyconfig.ServiceConfig, func(resolver melodycontainercontract.Resolver) (melodyconfigcontract.Configuration, error) {
        return configuration, nil
    }, melodycontainer.RegisterOptions{})

    melodycontainer.MustRegister(containerInstance, melodyvalidation.ServiceValidator, func(resolver melodycontainercontract.Resolver) (*melodyvalidation.Validator, error) {
        return melodyvalidation.NewValidator(melodyvalidation.ValidatorConfig{}), nil
    }, melodycontainer.RegisterOptions{})

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    httpRequest := httptest.NewRequest(nethttp.MethodPost, call.path, strings.NewReader(call.body))
    httpRequest.Header.Set("Content-Type", "application/json")
    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("access-token-test", time.Now()))

    firewall := melodysecurity.NewCompiledFirewall(melodysecurity.CompiledFirewallConfig{
        Name:               "main",
        Matcher:            melodysecurity.NewPathPrefixMatcher("/"),
        MatcherDescription: "prefix /",
    })
    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken(call.userIdentifier, []string{"ROLE_EDITOR"}, nil)),
    )

    response, handlerErr := call.handler(runtimeInstance, httptest.NewRecorder(), request)
    if nil != call.handlerErr {
        *call.handlerErr = handlerErr
    }

    if nil != handlerErr {
        return nethttp.StatusInternalServerError
    }

    if nil == response {
        t.Fatal("expected the door to answer")
    }

    return response.StatusCode()
}

func issueAs(t *testing.T, userIdentifier string) (int, *melodysecurity.InMemoryTokenStore) {
    t.Helper()

    tokenStore := melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{})

    status := callAccessTokenDoor(t, accessTokenDoorCall{
        handler:        IssueHandler(defaultTestDeviceIdentifierMaximumBytes),
        path:           "/access-token/issue/",
        body:           `{"deviceIdentifier":"phone"}`,
        userIdentifier: userIdentifier,
        tokenStore:     tokenStore,
    })

    return status, tokenStore
}

func issueDevice(t *testing.T, deviceIdentifier string) (int, *melodysecurity.InMemoryTokenStore) {
    t.Helper()

    tokenStore := melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{})
    body, _ := json.Marshal(map[string]string{"deviceIdentifier": deviceIdentifier})

    status := callAccessTokenDoor(t, accessTokenDoorCall{
        handler:        IssueHandler(defaultTestDeviceIdentifierMaximumBytes),
        path:           "/access-token/issue/",
        body:           string(body),
        userIdentifier: "user-2",
        tokenStore:     tokenStore,
    })

    return status, tokenStore
}

const defaultTestDeviceIdentifierMaximumBytes = 64

type lookupFailingTokenStore struct {
    *melodysecurity.InMemoryTokenStore
    lookupErr    error
    deleteErr    error
    deletedCount int
}

func (instance *lookupFailingTokenStore) Lookup(runtimeInstance melodyruntimecontract.Runtime, tokenString string) (melodysecuritycontract.Claims, bool, error) {
    return melodysecuritycontract.Claims{}, false, instance.lookupErr
}

func (instance *lookupFailingTokenStore) Delete(runtimeInstance melodyruntimecontract.Runtime, tokenString string) error {
    instance.deletedCount++

    if nil != instance.deleteErr {
        return instance.deleteErr
    }

    return instance.InMemoryTokenStore.Delete(runtimeInstance, tokenString)
}

type putFailingTokenStore struct {
    *melodysecurity.InMemoryTokenStore
    putErr error
}

func (instance *putFailingTokenStore) Put(_ melodyruntimecontract.Runtime, _ string, _ melodysecuritycontract.Claims, _ melodybound.Duration) error {
    return instance.putErr
}

func heldTokens(t *testing.T, store melodysecuritycontract.RevocableTokenStore, userIdentifier string) int {
    t.Helper()

    held, deleteErr := store.DeleteByUser(accessTokenTestRuntime(), userIdentifier)
    if nil != deleteErr {
        t.Fatalf("delete by user: %v", deleteErr)
    }

    return held
}

func accessTokenTestRuntime() melodyruntimecontract.Runtime {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

func TestIssueHandler_AFailedLookupOverAStoreThatCannotDeleteStillAnswersTheLookupError(t *testing.T) {
    lookupErr := errors.New("lookup unavailable")
    deleteErr := errors.New("delete unavailable")
    tokenStore := &lookupFailingTokenStore{InMemoryTokenStore: melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{}), lookupErr: lookupErr, deleteErr: deleteErr}

    var handlerErr error
    callAccessTokenDoor(t, accessTokenDoorCall{
        handler:        IssueHandler(defaultTestDeviceIdentifierMaximumBytes),
        path:           "/access-token/issue/",
        body:           `{"deviceIdentifier":"phone"}`,
        userIdentifier: "user-2",
        tokenStore:     tokenStore,
        handlerErr:     &handlerErr,
    })

    if false == errors.Is(handlerErr, lookupErr) || true == errors.Is(handlerErr, deleteErr) || 1 != tokenStore.deletedCount {
        t.Fatalf("expected the lookup error answered after one failed delete, got %v after %d deletes", handlerErr, tokenStore.deletedCount)
    }
}

func TestIssueHandler_AFailedPutIsReturned(t *testing.T) {
    putErr := errors.New("put unavailable")
    tokenStore := &putFailingTokenStore{InMemoryTokenStore: melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{}), putErr: putErr}

    var handlerErr error
    callAccessTokenDoor(t, accessTokenDoorCall{
        handler:        IssueHandler(defaultTestDeviceIdentifierMaximumBytes),
        path:           "/access-token/issue/",
        body:           `{"deviceIdentifier":"phone"}`,
        userIdentifier: "user-2",
        tokenStore:     tokenStore,
        handlerErr:     &handlerErr,
    })

    if false == errors.Is(handlerErr, putErr) {
        t.Fatalf("expected the failed put returned, got %v", handlerErr)
    }
}

func TestIssueHandler_TakesBackATokenWhoseAccountIsGoneAfterTheWrite(t *testing.T) {
    status, tokenStore := issueAs(t, "user-9")
    if nethttp.StatusUnauthorized != status {
        t.Fatalf("expected a gone account answered 401, got %d", status)
    }

    if left := heldTokens(t, tokenStore, "user-9"); 0 != left {
        t.Fatalf("expected no token left for the gone account, found %d", left)
    }

    status, tokenStore = issueAs(t, "user-2")
    if nethttp.StatusCreated != status {
        t.Fatalf("expected an existing account issued its token with 201, got %d", status)
    }

    if held := heldTokens(t, tokenStore, "user-2"); 1 != held {
        t.Fatalf("expected the existing account to hold the one token issued, found %d", held)
    }
}

func TestIssueHandler_RefusesADeviceIdentifierPastTheBound(t *testing.T) {
    status, tokenStore := issueDevice(t, strings.Repeat("a", defaultTestDeviceIdentifierMaximumBytes+1))
    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected an identifier past the bound refused with 400, got %d", status)
    }

    if held := heldTokens(t, tokenStore, "user-2"); 0 != held {
        t.Fatalf("expected no token stored for a refused identifier, found %d", held)
    }
}

func TestIssueHandler_RefusesADeviceIdentifierOutsideTheAlphabet(t *testing.T) {
    for _, deviceIdentifier := range []string{"", "my phone", "phone:1", "phone/1", "téléphone", " phone", "phone\n"} {
        if status, _ := issueDevice(t, deviceIdentifier); nethttp.StatusBadRequest != status {
            t.Fatalf("expected %q refused with 400, got %d", deviceIdentifier, status)
        }
    }
}

func TestIssueHandler_AdmitsTheBound(t *testing.T) {
    for _, deviceIdentifier := range []string{strings.Repeat("a", defaultTestDeviceIdentifierMaximumBytes), "Phone-1.work_2"} {
        if status, _ := issueDevice(t, deviceIdentifier); nethttp.StatusCreated != status {
            t.Fatalf("expected %q issued with 201, got %d", deviceIdentifier, status)
        }
    }
}

func TestRevokeDeviceHandler_RefusesADeviceIdentifierOutsideTheCeiling(t *testing.T) {
    for _, deviceIdentifier := range []string{strings.Repeat("a", DeviceIdentifierMaximumBytesCeiling+1), "phone:1"} {
        body, _ := json.Marshal(map[string]string{"deviceIdentifier": deviceIdentifier})

        status := callAccessTokenDoor(t, accessTokenDoorCall{
            handler:        RevokeDeviceHandler(),
            path:           "/access-token/revoke-device/",
            body:           string(body),
            userIdentifier: "user-2",
            tokenStore:     melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{}),
        })
        if nethttp.StatusBadRequest != status {
            t.Fatalf("expected %q refused with 400, got %d", deviceIdentifier, status)
        }
    }

    for _, deviceIdentifier := range []string{"phone", strings.Repeat("a", DeviceIdentifierMaximumBytesCeiling)} {
        body, _ := json.Marshal(map[string]string{"deviceIdentifier": deviceIdentifier})

        status := callAccessTokenDoor(t, accessTokenDoorCall{
            handler:        RevokeDeviceHandler(),
            path:           "/access-token/revoke-device/",
            body:           string(body),
            userIdentifier: "user-2",
            tokenStore:     melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{}),
        })
        if nethttp.StatusOK != status {
            t.Fatalf("expected %q inside the ceiling revoked with 200, got %d", deviceIdentifier, status)
        }
    }
}

func TestIssueHandler_DeletesTheTokenWhenTheLookupFails(t *testing.T) {
    tokenStore := &lookupFailingTokenStore{InMemoryTokenStore: melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{}), lookupErr: errors.New("store unavailable")}

    status := callAccessTokenDoor(t, accessTokenDoorCall{
        handler:        IssueHandler(defaultTestDeviceIdentifierMaximumBytes),
        path:           "/access-token/issue/",
        body:           `{"deviceIdentifier":"phone"}`,
        userIdentifier: "user-2",
        tokenStore:     tokenStore,
    })
    if nethttp.StatusInternalServerError != status {
        t.Fatalf("expected a failed lookup answered as an error, got %d", status)
    }

    if 1 != tokenStore.deletedCount || 0 != heldTokens(t, tokenStore, "user-2") {
        t.Fatalf("expected the minted token deleted once, deleted %d", tokenStore.deletedCount)
    }
}

func TestIssueHandler_DeletesTheTokenWhenTheLookupFindsNothing(t *testing.T) {
    tokenStore := &lookupFailingTokenStore{InMemoryTokenStore: melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{})}

    status := callAccessTokenDoor(t, accessTokenDoorCall{
        handler:        IssueHandler(defaultTestDeviceIdentifierMaximumBytes),
        path:           "/access-token/issue/",
        body:           `{"deviceIdentifier":"phone"}`,
        userIdentifier: "user-2",
        tokenStore:     tokenStore,
    })
    if nethttp.StatusInternalServerError != status {
        t.Fatalf("expected a lookup that found nothing answered 500, got %d", status)
    }

    if 1 != tokenStore.deletedCount || 0 != heldTokens(t, tokenStore, "user-2") {
        t.Fatalf("expected the minted token deleted once, deleted %d", tokenStore.deletedCount)
    }
}

func TestRevokeUserHandler_RefusesAnIdentifierNoKeyCanCarry(t *testing.T) {
    body, _ := json.Marshal(map[string]string{"userIdentifier": strings.Repeat("u", 256)})

    status := callAccessTokenDoor(t, accessTokenDoorCall{
        handler:        RevokeUserHandler(),
        path:           "/access-token/revoke-user/",
        body:           string(body),
        userIdentifier: "user-1",
        tokenStore:     melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{}),
    })
    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected an identifier no key can carry refused with 400, got %d", status)
    }
}

func TestWithdrawMintedToken_ACancelledRequestStillWithdrawsTheToken(t *testing.T) {
    store := melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{})
    if putErr := store.Put(accessTokenTestRuntime(), "minted", melodysecuritycontract.Claims{UserIdentifier: "user-9"}, melodybound.UnlimitedDuration()); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    withdrawMintedToken(melodyruntime.New(cancelledContext, containerInstance.NewScope(), containerInstance), store, "minted")

    if _, found, _ := store.Lookup(accessTokenTestRuntime(), "minted"); true == found {
        t.Fatalf("expected the minted token withdrawn although the request was cancelled")
    }
}
