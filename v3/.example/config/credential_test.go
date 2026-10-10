package config

import (
    "fmt"
    "strings"
    "testing"
    "time"

    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

const ownJwtSecret = "an-operator-chosen-signing-secret-of-its-own"

/* in development a blank key takes the committed value and a value of its own is kept */
func TestCredential_DevelopmentTakesTheCommittedValueForABlankKey(t *testing.T) {
    if exampleJwtSecret != moduleWithEnvironment(t, map[string]string{}).credential(environmentKeyJwtSecret, exampleJwtSecret) {
        t.Fatal("expected a blank key to take the committed value in development")
    }

    moduleInstance := moduleWithEnvironment(t, map[string]string{environmentKeyJwtSecret: ownJwtSecret})
    if ownJwtSecret != moduleInstance.credential(environmentKeyJwtSecret, exampleJwtSecret) {
        t.Fatal("expected a value of its own kept in development")
    }
}

/* outside development the two signing secrets refuse the boot blank or committed, naming the key and never a value, and boot with a value of their own */
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
            moduleInstance := moduleWithEnvironment(t, map[string]string{melodyconfig.EnvKey: melodyconfig.EnvProduction, credentialCase.environmentKey: value})

            refusal := recoveredRefusal(t, func() { credentialCase.build(moduleInstance) })
            if nil == refusal || false == strings.HasPrefix(refusal.Error(), credentialCase.environmentKey+" ") {
                t.Fatalf("%s=%q: expected the boot refused naming the key, got %v", credentialCase.environmentKey, value, refusal)
            }

            logged := fmt.Sprintf("%v %v", refusal, melodyexception.LogContext(refusal))
            if true == strings.Contains(logged, credentialCase.developmentValue) {
                t.Fatalf("%s: the refusal carries the value: %v", credentialCase.environmentKey, logged)
            }
        }

        moduleInstance := moduleWithEnvironment(t, map[string]string{melodyconfig.EnvKey: melodyconfig.EnvProduction, credentialCase.environmentKey: "a-value-of-its-own-for-" + credentialCase.environmentKey})
        if refusal := recoveredRefusal(t, func() { credentialCase.build(moduleInstance) }); nil != refusal {
            t.Fatalf("%s: expected a value of its own to boot outside development, got %v", credentialCase.environmentKey, refusal)
        }
    }
}

/* the token firewall outside development verifies with the configured secret: a token signed with the committed constant, carrying the roles a scope grants, is refused, and one signed with the configured secret is accepted */
func TestBuildTokenAuth_OutsideDevelopmentRefusesATokenSignedWithTheCommittedSecret(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{melodyconfig.EnvKey: melodyconfig.EnvProduction, environmentKeyJwtSecret: ownJwtSecret})
    moduleInstance.buildTokenAuth()

    issuedAt := time.Unix(1700000000, 0).UTC()
    config := exampleJwtConfig(moduleInstance.jwtSecret)
    config.Clock = melodyclock.NewFrozenClock(issuedAt.Add(time.Minute))
    validator := melodysecurity.NewJwtTokenValidatorWithRevocationEpoch(config, fixedRevocationEpochStore{})

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
}

/* the internal firewall verifies under the configured key id, application and secret; blank names take the example's own */
func TestBuildInternalAuth_ReadsTheKeyIdApplicationAndSecretFromConfiguration(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{
        melodyconfig.EnvKey:              melodyconfig.EnvProduction,
        environmentKeyInternalAuthKeyId:  "erp-key-7",
        environmentKeyInternalAuthApp:    "erp-service",
        environmentKeyInternalAuthSecret: "an-operator-chosen-shared-secret",
    })
    moduleInstance.buildInternalAuth()

    secret, found := moduleInstance.hmacSecrets.Secret("erp-key-7")
    application, _ := moduleInstance.hmacSecrets.AppForKeyId("erp-key-7")
    if false == found || "an-operator-chosen-shared-secret" != string(secret) || "erp-service" != application || "erp-key-7" != moduleInstance.hmacSecrets.CurrentKeyId() {
        t.Fatalf("expected the configured key, got %q %v %q", secret, found, application)
    }

    if _, found := moduleInstance.hmacSecrets.Secret(internalCallerKeyId); true == found {
        t.Fatal("expected the committed key id unknown once another is configured")
    }

    developmentInstance := moduleWithEnvironment(t, map[string]string{})
    developmentInstance.buildInternalAuth()
    if developmentInstance.hmacSecrets.CurrentKeyId() != internalCallerKeyId || internalCallerApp != developmentInstance.internalAuthApp {
        t.Fatalf("expected blank names to take the example's own, got %q %q", developmentInstance.hmacSecrets.CurrentKeyId(), developmentInstance.internalAuthApp)
    }
}
