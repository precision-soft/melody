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

        /* the documented format is xxxxx-xxxxx: five base32 chars, a dash, five base32 chars (eleven runes total). */
        if 11 != len(code) || '-' != code[5] {
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

/* Resolve must expose exactly the values Verify runs with: zero-value defaults filled in and the skew clamped to the same ceiling Verify enforces, so a caller sizing a replay window from Resolve never diverges from what Verify accepts. */
func TestResolveFillsDefaultsAndClampsSkew(t *testing.T) {
    resolved := totp.Config{}.Resolve()
    if 30 != resolved.Period || 6 != resolved.Digits || 1 != resolved.Skew {
        t.Fatalf("expected zero-value defaults (30/6/1), got %d/%d/%d", resolved.Period, resolved.Digits, resolved.Skew)
    }

    clamped := totp.Config{Period: 60, Digits: 8, Skew: 1_000_000}.Resolve()
    if 10 != clamped.Skew {
        t.Fatalf("expected a huge skew to clamp to 10, got %d", clamped.Skew)
    }

    if 60 != clamped.Period || 8 != clamped.Digits {
        t.Fatalf("expected in-range period/digits preserved, got %d/%d", clamped.Period, clamped.Digits)
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

func TestVerifyAt_ToleratesWhitespaceInTheSubmittedCode(t *testing.T) {
    at := time.Unix(1111111109, 0)

    code, codeErr := totp.GenerateCodeAt(rfc6238Secret, at, totp.Config{})
    if nil != codeErr {
        t.Fatalf("unexpected code error: %v", codeErr)
    }

    /* authenticator display format, plain copy/paste padding, and the NBSP mobile keyboards produce */
    variants := []string{
        " " + code + " ",
        code[:3] + " " + code[3:],
        code[:3] + " " + code[3:],
        "\t" + code,
    }

    for _, variant := range variants {
        valid, verifyErr := totp.VerifyAt(rfc6238Secret, variant, at, totp.Config{})
        if nil != verifyErr {
            t.Fatalf("unexpected verify error for %q: %v", variant, verifyErr)
        }

        if false == valid {
            t.Fatalf("expected the whitespace variant %q to verify", variant)
        }
    }
}

func TestVerifyAt_StillRejectsWrongAndShortCodes(t *testing.T) {
    at := time.Unix(1111111109, 0)

    code, codeErr := totp.GenerateCodeAt(rfc6238Secret, at, totp.Config{})
    if nil != codeErr {
        t.Fatalf("unexpected code error: %v", codeErr)
    }

    /* flip the last digit so the normalized form is a well-formed but wrong code */
    flipped := code[:5] + string('0'+(code[5]-'0'+1)%10)

    for _, invalid := range []string{"12 34", "  ", flipped[:3] + " " + flipped[3:]} {
        valid, verifyErr := totp.VerifyAt(rfc6238Secret, invalid, at, totp.Config{})
        if nil != verifyErr {
            t.Fatalf("unexpected verify error for %q: %v", invalid, verifyErr)
        }

        if true == valid {
            t.Fatalf("expected %q to be rejected", invalid)
        }
    }
}

/** @info NormalizeCode is the single normalization Verify applies; callers that key a replay guard on an accepted code must use it, so it is exported and must stay in step with VerifyAt. */
func TestNormalizeCode_StripsEveryWhitespaceForm(t *testing.T) {
    /* the last variant carries a real non-breaking space (U+00A0), the separator a mobile copy/paste of a displayed code keeps */
    for _, variant := range []string{"123456", "123 456", " 123456 ", "1 2 3 4 5 6", "123\t456", "123 456"} {
        if normalized := totp.NormalizeCode(variant); "123456" != normalized {
            t.Fatalf("expected %q to normalize to \"123456\", got %q", variant, normalized)
        }
    }
}
