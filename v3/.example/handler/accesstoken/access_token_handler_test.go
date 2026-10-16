package accesstoken

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodyconfigcontract "github.com/precision-soft/melody/v3/config/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    melodyvalidation "github.com/precision-soft/melody/v3/validation"
)

/* emptyEnvironmentSource is a .env holding nothing, so the configuration the binding reads its limits from is the framework's defaults */
type emptyEnvironmentSource struct{}

func (instance emptyEnvironmentSource) Load() (map[string]string, error) {
    return map[string]string{}, nil
}

/* issueAs drives the issue door for a caller the session resolved to the given account, over a directory holding the example's accounts and an in-memory token store, and answers the status and the store */
func issueAs(t *testing.T, userIdentifier string) (int, *melodysecurity.InMemoryTokenStore) {
    t.Helper()

    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil).WithAccountSeed())
    if nil != repositoryErr {
        t.Fatalf("build the user repository: %v", repositoryErr)
    }

    tokenStore := melodysecurity.NewInMemoryTokenStore()

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(containerInstance, repository.ServiceUserRepository, func(resolver melodycontainercontract.Resolver) (repository.UserRepository, error) {
        return userRepository, nil
    })
    melodycontainer.MustRegister(containerInstance, melodyrueidis.ServiceTokenStore, func(resolver melodycontainercontract.Resolver) (melodysecuritycontract.EpochRevocableTokenStore, error) {
        return tokenStore, nil
    })
    melodycontainer.MustRegister(containerInstance, melodylogging.ServiceLogger, func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
        return melodylogging.NewNopLogger(), nil
    })
    melodycontainer.MustRegister(containerInstance, melodyclock.ServiceClock, func(resolver melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
        return melodyclock.NewSystemClock(), nil
    })

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
    })

    melodycontainer.MustRegister(containerInstance, melodyvalidation.ServiceValidator, func(resolver melodycontainercontract.Resolver) (*melodyvalidation.Validator, error) {
        return melodyvalidation.NewValidator(), nil
    })

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/access-token/issue/", strings.NewReader(`{"deviceIdentifier":"phone"}`))
    httpRequest.Header.Set("Content-Type", "application/json")
    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("issue-test", time.Now()))

    firewall := melodysecurity.NewCompiledFirewall(
        "main",
        melodysecurity.NewPathPrefixMatcher("/"),
        "prefix /",
        nil, nil, nil, nil, nil, nil, nil,
        "", "",
        nil, nil,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
    )
    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken(userIdentifier, []string{"ROLE_EDITOR"})),
    )

    response, handlerErr := IssueHandler()(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response {
        t.Fatalf("expected the issue door to answer, got %v, %v", response, handlerErr)
    }

    return response.StatusCode(), tokenStore
}

/* a session whose account is gone by the time the token is stored — deleted between the session's resolution and the write — is answered 401 and the token it wrote is taken back, so nothing of the account is left in the store; an account that still exists is issued its token */
func TestIssueHandler_TakesBackATokenWhoseAccountIsGoneAfterTheWrite(t *testing.T) {
    status, tokenStore := issueAs(t, "user-9")
    if nethttp.StatusUnauthorized != status {
        t.Fatalf("expected a gone account answered 401, got %d", status)
    }

    if left := tokenStore.DeleteByUser("user-9"); 0 != left {
        t.Fatalf("expected no token left for the gone account, found %d", left)
    }

    status, tokenStore = issueAs(t, "user-2")
    if nethttp.StatusCreated != status {
        t.Fatalf("expected an existing account issued its token with 201, got %d", status)
    }

    if held := tokenStore.DeleteByUser("user-2"); 1 != held {
        t.Fatalf("expected the existing account to hold the one token issued, found %d", held)
    }
}
