package security

import (
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func TestNewJwtSecretKeySet_EmptySecret_RefusedNamingTheArgument(t *testing.T) {
    for _, secret := range [][]byte{nil, {}} {
        func() {
            defer func() {
                refusal, isRefusal := recover().(*exception.Error)
                if false == isRefusal || "jwt secret is empty" != refusal.Message() || "secret" != refusal.Context()["argument"] {
                    t.Fatalf("expected the empty secret refused naming the argument, got %v", refusal)
                }
            }()

            NewJwtSecretKeySet(secret, JwtSecretKeySetConfig{})
        }()
    }
}

func TestNewJwtSecretKeySet_ShortSecret_RefusedNamingTheArgument(t *testing.T) {
    expectShortSecretRefusal(t, "argument", "secret", 31, 32, "", func() {
        NewJwtSecretKeySet([]byte("short-secret-of-31-bytes-long!!"), JwtSecretKeySetConfig{})
    })

    NewJwtSecretKeySet([]byte(thirtyTwoByteSecret), JwtSecretKeySetConfig{})

    expectShortSecretRefusal(t, "argument", "secret", 32, 40, "", func() {
        NewJwtSecretKeySet([]byte(thirtyTwoByteSecret), JwtSecretKeySetConfig{MinimumSecretLength: bound.Of(40)})
    })

    NewJwtSecretKeySet([]byte(strings.Repeat("k", 40)), JwtSecretKeySetConfig{MinimumSecretLength: bound.Of(40)})
}

func TestNewJwtSecretKeySet_MinimumBelowTheFloor_Refused(t *testing.T) {
    expectPanicNamingField(t, "JwtSecretKeySetConfig.MinimumSecretLength", func() {
        NewJwtSecretKeySet([]byte(thirtyTwoByteSecret), JwtSecretKeySetConfig{MinimumSecretLength: bound.Of(31)})
    })

    expectPanicNamingField(t, "JwtSecretKeySetConfig.MinimumSecretLength", func() {
        NewJwtSecretKeySet([]byte(thirtyTwoByteSecret), JwtSecretKeySetConfig{MinimumSecretLength: bound.UnlimitedCount()})
    })
}

/* the set keeps its own copy of the secret, and hands each caller a copy of its own: by reference, zeroing or rotating either slice in place would change what every later token verifies against */
func TestNewJwtSecretKeySet_CopiesTheSecret(t *testing.T) {
    secret := []byte("copy-me-before-i-change-padded-to-the-hs256-minimum")
    tokenString := signJwtHs256(append([]byte{}, secret...), map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    keySet := NewJwtSecretKeySet(secret, JwtSecretKeySetConfig{})
    validator := NewJwtTokenValidator(keySet, JwtConfig{})

    for index := range secret {
        secret[index] = 0
    }

    answered, _ := keySet.VerificationKeys(testRuntime(), securitycontract.JwtKeyQuery{Algorithm: JwtAlgorithmHs256})
    for index := range answered[0].Secret {
        answered[0].Secret[index] = 0
    }

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("mutating the caller's or an answered secret slice changed what the validator verifies against: %v", validateErr)
    }
}

func TestJwtSecretKeySet_AnswersItsKeyWhateverTheKeyId(t *testing.T) {
    secret := []byte(thirtyTwoByteSecret)
    keySet := NewJwtSecretKeySet(secret, JwtSecretKeySetConfig{})

    for _, query := range []securitycontract.JwtKeyQuery{{Algorithm: JwtAlgorithmHs256}, {KeyId: "key-2026", Algorithm: JwtAlgorithmHs256}, {KeyId: "unknown", Algorithm: "HS512"}} {
        keyId := query.KeyId
        keys, keysErr := keySet.VerificationKeys(testRuntime(), query)
        if nil != keysErr || 1 != len(keys) {
            t.Fatalf("kid %q: expected the one key, got %v %v", keyId, keys, keysErr)
        }

        if "" != keys[0].KeyId || JwtAlgorithmHs256 != keys[0].Algorithm || string(secret) != string(keys[0].Secret) {
            t.Fatalf("kid %q: expected the HS256 key of the secret, got %+v", keyId, keys[0])
        }
    }

    tokenString := signJwtWithHeader(secret, map[string]any{"alg": "HS256", "typ": "JWT", "kid": "key-2026"}, map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(time.Hour).Unix(),
    })
    if _, validateErr := NewJwtTokenValidator(keySet, JwtConfig{}).Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("expected a token naming a kid verified against the one secret, got %v", validateErr)
    }
}
