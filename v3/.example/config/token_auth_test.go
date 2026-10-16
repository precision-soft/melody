package config

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "testing"
    "time"

    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

/* fixedRevocationEpochStore answers one revocation instant for every account */
type fixedRevocationEpochStore struct {
    epoch time.Time
}

func (instance fixedRevocationEpochStore) RevokeBefore(userIdentifier string, deviceIdentifier string, instant time.Time) {
}

func (instance fixedRevocationEpochStore) RevocationEpoch(runtimeInstance melodyruntimecontract.Runtime, userIdentifier string, deviceIdentifier string) (time.Time, error) {
    return instance.epoch, nil
}

func signedTestJwt(t *testing.T, secret []byte, claims map[string]any) string {
    t.Helper()

    headerBytes, _ := json.Marshal(map[string]any{"alg": "HS256", "typ": "JWT"})
    claimBytes, marshalErr := json.Marshal(claims)
    if nil != marshalErr {
        t.Fatalf("marshal claims: %v", marshalErr)
    }

    signingInput := base64.RawURLEncoding.EncodeToString(headerBytes) + "." + base64.RawURLEncoding.EncodeToString(claimBytes)

    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(signingInput))

    return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

/* a revoke-all drawn at an instant refuses a token issued up to two seconds after it, the skew the replicas' clocks may differ by, and accepts one issued past that */
func TestExampleJwtConfig_RefusesATokenIssuedInsideTheSkewAfterARevocation(t *testing.T) {
    revokedAt := time.Unix(1700000000, 0).UTC()

    config := exampleJwtConfig([]byte(exampleJwtSecret))
    config.Clock = melodyclock.NewFrozenClock(revokedAt.Add(10 * time.Second))

    validator := melodysecurity.NewJwtTokenValidatorWithRevocationEpoch(config, fixedRevocationEpochStore{epoch: revokedAt})

    tokenIssuedAt := func(issuedAt time.Time) string {
        return signedTestJwt(t, []byte(exampleJwtSecret), map[string]any{
            "sub":   "user-1",
            "roles": []string{"ROLE_USER"},
            "iat":   issuedAt.Unix(),
            "exp":   issuedAt.Add(time.Hour).Unix(),
        })
    }

    if _, validateErr := validator.Validate(nil, tokenIssuedAt(revokedAt.Add(time.Second))); nil == validateErr {
        t.Fatal("expected a token issued a second after the revocation refused inside the skew")
    }

    if _, validateErr := validator.Validate(nil, tokenIssuedAt(revokedAt.Add(3*time.Second))); nil != validateErr {
        t.Fatalf("expected a token issued past the skew accepted, got %v", validateErr)
    }
}
