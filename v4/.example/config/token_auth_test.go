package config

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "testing"
    "time"

    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

type fixedRevocationEpochStore struct {
    epoch time.Time
}

func (instance fixedRevocationEpochStore) RevokeBefore(
    _ melodycontainercontract.Runtime,
    _ string,
    _ time.Time,
    _ melodysecuritycontract.RevocationOptions,
) error {
    return nil
}

func (instance fixedRevocationEpochStore) RevocationEpoch(runtimeInstance melodycontainercontract.Runtime, userIdentifier string, options melodysecuritycontract.RevocationOptions) (time.Time, error) {
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

func TestExampleJwtConfig_RefusesATokenIssuedInsideTheSkewAfterARevocation(t *testing.T) {
    revokedAt := time.Unix(1700000000, 0).UTC()

    config := exampleJwtConfig()
    config.Clock = melodyclock.NewFrozenClock(revokedAt.Add(10 * time.Second))

    config.RevocationEpochStore = fixedRevocationEpochStore{epoch: revokedAt}
    validator := melodysecurity.NewJwtTokenValidator(exampleJwtKeySet([]byte(exampleJwtSecret)), config)

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
