package totp_test

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/security/totp"
)

/* rfc6238Secret is the ASCII secret "12345678901234567890" encoded as base32, from the RFC 6238 test vectors. */
const rfc6238Secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestVerifyAt_Rfc6238KnownAnswer(t *testing.T) {
    /* the RFC's SHA-1 8-digit code at T=59 is 94287082; truncated to 6 digits it is 287082 */
    ok, verifyErr := totp.VerifyAt(rfc6238Secret, "287082", time.Unix(59, 0), totp.Config{})
    if nil != verifyErr {
        t.Fatalf("verify: %v", verifyErr)
    }

    if false == ok {
        t.Fatal("expected the RFC 6238 known-answer code to verify")
    }
}

func TestGenerateAndVerifyRoundTrip(t *testing.T) {
    secret, secretErr := totp.GenerateSecret()
    if nil != secretErr {
        t.Fatalf("generate secret: %v", secretErr)
    }

    now := time.Now()

    code, codeErr := totp.GenerateCodeAt(secret, now, totp.Config{})
    if nil != codeErr {
        t.Fatalf("generate code: %v", codeErr)
    }

    ok, verifyErr := totp.VerifyAt(secret, code, now, totp.Config{})
    if nil != verifyErr || false == ok {
        t.Fatalf("expected a freshly generated code to verify, ok=%v err=%v", ok, verifyErr)
    }
}

func TestVerifyAt_AcceptsCodeWithinSkew(t *testing.T) {
    secret, _ := totp.GenerateSecret()
    now := time.Unix(1_700_000_000, 0)

    /* a code from the previous 30s step must still verify with the default ±1 skew */
    previous, _ := totp.GenerateCodeAt(secret, now.Add(-30*time.Second), totp.Config{})

    ok, _ := totp.VerifyAt(secret, previous, now, totp.Config{})
    if false == ok {
        t.Fatal("expected a previous-step code to verify within skew")
    }
}

/* negative control: a code two steps away is outside the default skew window. */
func TestVerifyAt_RejectsCodeOutsideSkew(t *testing.T) {
    secret, _ := totp.GenerateSecret()
    now := time.Unix(1_700_000_000, 0)

    stale, _ := totp.GenerateCodeAt(secret, now.Add(-90*time.Second), totp.Config{})

    ok, _ := totp.VerifyAt(secret, stale, now, totp.Config{})
    if true == ok {
        t.Fatal("expected a code three steps old to be rejected")
    }
}

func TestVerifyAt_RejectsWrongCode(t *testing.T) {
    secret, _ := totp.GenerateSecret()

    ok, _ := totp.VerifyAt(secret, "000000", time.Unix(59, 0), totp.Config{})
    if true == ok {
        /* astronomically unlikely to be the real code; guards against an always-true bug */
        t.Fatal("expected an arbitrary code to be rejected")
    }
}

func TestOtpauthURI_ContainsSecretAndIssuer(t *testing.T) {
    uri := totp.OtpauthURI("Melody", "alice@example.com", rfc6238Secret, totp.Config{})

    if false == containsAll(uri, "otpauth://totp/", "secret="+rfc6238Secret, "issuer=Melody", "period=30", "digits=6") {
        t.Fatalf("unexpected otpauth uri: %s", uri)
    }
}

func TestGenerateRecoveryCodes_UniqueAndFormatted(t *testing.T) {
    codes, codesErr := totp.GenerateRecoveryCodes(8)
    if nil != codesErr {
        t.Fatalf("recovery codes: %v", codesErr)
    }

    if 8 != len(codes) {
        t.Fatalf("expected 8 recovery codes, got %d", len(codes))
    }

    seen := map[string]bool{}
    for _, code := range codes {
        if true == seen[code] {
            t.Fatalf("duplicate recovery code %q", code)
        }
        seen[code] = true

        if 9 != len(code) || '-' != code[5] {
            t.Fatalf("unexpected recovery code format %q", code)
        }
    }
}

/* an out-of-range digit count (which would otherwise overflow the uint32 modulo) is clamped to the default rather than producing a broken or panicking code. */
func TestVerifyAt_ClampsOutOfRangeDigits(t *testing.T) {
    secret, _ := totp.GenerateSecret()
    now := time.Unix(1_700_000_000, 0)

    code, codeErr := totp.GenerateCodeAt(secret, now, totp.Config{Digits: 10})
    if nil != codeErr {
        t.Fatalf("generate code: %v", codeErr)
    }

    if 6 != len(code) {
        t.Fatalf("expected an out-of-range digit count to clamp to 6, got a %d-digit code", len(code))
    }

    ok, _ := totp.VerifyAt(secret, code, now, totp.Config{Digits: 10})
    if false == ok {
        t.Fatal("expected the clamped code to verify")
    }
}

func containsAll(haystack string, needles ...string) bool {
    for _, needle := range needles {
        found := false
        for index := 0; index+len(needle) <= len(haystack); index++ {
            if haystack[index:index+len(needle)] == needle {
                found = true

                break
            }
        }
        if false == found {
            return false
        }
    }

    return true
}
