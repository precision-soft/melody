package config

import (
    "context"
    "fmt"
    "strings"
    "testing"
    "time"

    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

const ownJwtSecret = "an-operator-chosen-signing-secret-of-its-own"

func TestCredential_DevelopmentTakesTheCommittedValueForABlankKey(t *testing.T) {
    if exampleJwtSecret != moduleWithEnvironment(t, map[string]string{}).credential(environmentKeyJwtSecret, parameterJwtSecret, exampleJwtSecret) {
        t.Fatal("expected a blank key to take the committed value in development")
    }

    moduleInstance := moduleWithEnvironment(t, map[string]string{environmentKeyJwtSecret: ownJwtSecret})
    if ownJwtSecret != moduleInstance.credential(environmentKeyJwtSecret, parameterJwtSecret, exampleJwtSecret) {
        t.Fatal("expected a value of its own kept in development")
    }
}

func TestCredential_OutsideDevelopmentRefusesABlankOrCommittedValueNamingOnlyTheKey(t *testing.T) {
    for _, credentialCase := range []struct {
        environmentKey   string
        developmentValue string
        build            func(moduleInstance *Module)
    }{
        {environmentKeyJwtSecret, exampleJwtSecret, func(moduleInstance *Module) { moduleInstance.buildTokenAuth() }},
        {environmentKeyInternalAuthSecret, internalCallerSecret, func(moduleInstance *Module) { moduleInstance.buildInternalAuth() }},
    } {
        for _, value := range []string{"", "  ", credentialCase.developmentValue} {
            moduleInstance := moduleWithEnvironment(t, map[string]string{melodyconfig.EnvironmentNameKey: melodyconfig.EnvironmentProduction, credentialCase.environmentKey: value})

            refusal := recoveredRefusal(t, func() { credentialCase.build(moduleInstance) })
            if nil == refusal || false == strings.HasPrefix(refusal.Error(), credentialCase.environmentKey+" ") {
                t.Fatalf("%s=%q: expected the boot refused naming the key, got %v", credentialCase.environmentKey, value, refusal)
            }

            logged := fmt.Sprintf("%v %v", refusal, melodyexception.LogContext(refusal, melodyexception.LogContextOptions{}))
            if true == strings.Contains(logged, credentialCase.developmentValue) {
                t.Fatalf("%s: the refusal carries the value: %v", credentialCase.environmentKey, logged)
            }
        }

        moduleInstance := moduleWithEnvironment(t, map[string]string{melodyconfig.EnvironmentNameKey: melodyconfig.EnvironmentProduction, credentialCase.environmentKey: "a-value-of-its-own-for-" + credentialCase.environmentKey})
        if refusal := recoveredRefusal(t, func() { credentialCase.build(moduleInstance) }); nil != refusal {
            t.Fatalf("%s: expected a value of its own to boot outside development, got %v", credentialCase.environmentKey, refusal)
        }
    }
}

func TestBuildTokenAuth_OutsideDevelopmentRefusesATokenSignedWithTheCommittedSecret(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{melodyconfig.EnvironmentNameKey: melodyconfig.EnvironmentProduction, environmentKeyJwtSecret: ownJwtSecret})
    moduleInstance.buildTokenAuth()

    issuedAt := time.Unix(1700000000, 0).UTC()
    config := exampleJwtConfig()
    config.Clock = melodyclock.NewFrozenClock(issuedAt.Add(time.Minute))
    config.RevocationEpochStore = fixedRevocationEpochStore{}
    validator := melodysecurity.NewJwtTokenValidator(exampleJwtKeySet(moduleInstance.jwtSecret), config)

    tokenSignedWith := func(secret string) string {
        return signedTestJwt(t, []byte(secret), map[string]any{
            "sub":   "user-1",
            "scope": map[string]any{"roles": []string{"ROLE_ADMIN", "ROLE_ALLOWED_TO_SWITCH"}},
            "iat":   issuedAt.Unix(),
            "exp":   issuedAt.Add(time.Hour).Unix(),
        })
    }

    if _, validateErr := validator.Validate(nil, tokenSignedWith(exampleJwtSecret)); nil == validateErr {
        t.Fatal("expected a token signed with the committed secret refused")
    }

    if _, validateErr := validator.Validate(nil, tokenSignedWith(ownJwtSecret)); nil != validateErr {
        t.Fatalf("expected a token signed with the configured secret accepted, got %v", validateErr)
    }

    tokenWithoutIssuedAt := func(secret string) string {
        return signedTestJwt(t, []byte(secret), map[string]any{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()})
    }

    if _, validateErr := moduleInstance.tokenValidator.Validate(nil, tokenWithoutIssuedAt(exampleJwtSecret)); nil == validateErr || "jwt signature mismatch" != validateErr.Error() {
        t.Fatalf("expected the module's validator to refuse the committed secret's signature, got %v", validateErr)
    }

    if _, validateErr := moduleInstance.tokenValidator.Validate(nil, tokenWithoutIssuedAt(ownJwtSecret)); nil == validateErr || "jwt signature mismatch" == validateErr.Error() {
        t.Fatalf("expected the module's validator to verify the configured secret's signature, got %v", validateErr)
    }
}

func TestBuildInternalAuth_ReadsTheKeyIdApplicationAndSecretFromConfiguration(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{
        melodyconfig.EnvironmentNameKey:       melodyconfig.EnvironmentProduction,
        environmentKeyInternalAuthKeyId:       " erp-key-7 ",
        environmentKeyInternalAuthApplication: " erp-service",
        environmentKeyInternalAuthSecret:      "an-operator-chosen-shared-secret",
    })
    moduleInstance.buildInternalAuth()

    secret, found, _ := moduleInstance.hmacSecrets.Secret(hmacTestRuntime(), "erp-key-7")
    application, _, _ := moduleInstance.hmacSecrets.ApplicationForKeyId(hmacTestRuntime(), "erp-key-7")
    if false == found || "an-operator-chosen-shared-secret" != string(secret) || "erp-service" != application || "erp-key-7" != currentKeyIdOf(t, moduleInstance) {
        t.Fatalf("expected the configured key, got %q %v %q", secret, found, application)
    }

    if _, found, _ := moduleInstance.hmacSecrets.Secret(hmacTestRuntime(), internalCallerKeyId); true == found {
        t.Fatal("expected the committed key id unknown once another is configured")
    }

    developmentInstance := moduleWithEnvironment(t, map[string]string{})
    developmentInstance.buildInternalAuth()
    if currentKeyIdOf(t, developmentInstance) != internalCallerKeyId || internalCallerApplication != developmentInstance.internalAuthApplication {
        t.Fatalf("expected blank names to take the example's own, got %q %q", currentKeyIdOf(t, developmentInstance), developmentInstance.internalAuthApplication)
    }
}

func currentKeyIdOf(t *testing.T, moduleInstance *Module) string {
    t.Helper()

    keyId, keyIdErr := moduleInstance.hmacSecrets.CurrentKeyId(hmacTestRuntime())
    if nil != keyIdErr {
        t.Fatalf("current key id: %v", keyIdErr)
    }

    return keyId
}

func hmacTestRuntime() melodycontainercontract.Runtime {
    return melodyruntime.NewRun(context.Background(), melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope())
}
