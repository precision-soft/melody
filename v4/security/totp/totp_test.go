package totp

import (
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/exception"
)

/* rfc6238Secret is the ASCII secret "12345678901234567890" encoded as base32, from the RFC 6238 test vectors. */
const rfc6238Secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestVerify_Rfc6238KnownAnswer(t *testing.T) {
    /* the RFC's SHA-1 8-digit code at T=59 is 94287082; truncated to 6 digits it is 287082 */
    ok, verifyErr := Verify(rfc6238Secret, "287082", Config{Clock: clock.NewFrozenClock(time.Unix(59, 0))})
    if nil != verifyErr {
        t.Fatalf("verify: %v", verifyErr)
    }

    if false == ok {
        t.Fatal("expected the RFC 6238 known-answer code to verify")
    }
}

func TestGenerateAndVerifyRoundTrip(t *testing.T) {
    secret, secretErr := GenerateSecret()
    if nil != secretErr {
        t.Fatalf("generate secret: %v", secretErr)
    }

    now := time.Now()

    code, codeErr := GenerateCodeAt(secret, now, Config{})
    if nil != codeErr {
        t.Fatalf("generate code: %v", codeErr)
    }

    ok, verifyErr := Verify(secret, code, Config{Clock: clock.NewFrozenClock(now)})
    if nil != verifyErr || false == ok {
        t.Fatalf("expected a freshly generated code to verify, ok=%v err=%v", ok, verifyErr)
    }
}

func TestVerify_AcceptsCodeWithinSkew(t *testing.T) {
    secret, _ := GenerateSecret()
    now := time.Unix(1_700_000_000, 0)

    /* a code from the previous 30s step must still verify with the default ±1 skew */
    previous, _ := GenerateCodeAt(secret, now.Add(-30*time.Second), Config{})

    ok, _ := Verify(secret, previous, Config{Clock: clock.NewFrozenClock(now)})
    if false == ok {
        t.Fatal("expected a previous-step code to verify within skew")
    }
}

/* the negative control sits on the boundary, at two steps: the default window is one step either way, so two is the FIRST offset that must be refused. Probing at three would leave a window one step too wide indistinguishable from the right one — a loop bound written Skew+1 accepts the two-step code. Both directions, because the loop is symmetric and a bound wrong on one side only would otherwise hide. */
func TestVerify_RejectsCodeOutsideSkew(t *testing.T) {
    secret, _ := GenerateSecret()
    now := time.Unix(1_700_000_000, 0)

    stale, _ := GenerateCodeAt(secret, now.Add(-60*time.Second), Config{})

    ok, _ := Verify(secret, stale, Config{Clock: clock.NewFrozenClock(now)})
    if true == ok {
        t.Fatal("expected a code two steps old — the first offset outside the default window — to be rejected")
    }

    ahead, _ := GenerateCodeAt(secret, now.Add(60*time.Second), Config{})

    okAhead, _ := Verify(secret, ahead, Config{Clock: clock.NewFrozenClock(now)})
    if true == okAhead {
        t.Fatal("expected a code two steps ahead to be rejected as well")
    }
}

func TestVerify_RejectsWrongCode(t *testing.T) {
    secret, _ := GenerateSecret()

    ok, _ := Verify(secret, "000000", Config{Clock: clock.NewFrozenClock(time.Unix(59, 0))})
    if true == ok {
        /* astronomically unlikely to be the real code; guards against an always-true bug */
        t.Fatal("expected an arbitrary code to be rejected")
    }
}

func TestOtpauthUri_ContainsSecretAndIssuer(t *testing.T) {
    uri, uriErr := OtpauthUri("Melody", "alice@example.com", rfc6238Secret, Config{})
    if nil != uriErr {
        t.Fatalf("otpauth uri: %v", uriErr)
    }

    if false == containsAll(uri, "otpauth://totp/", "secret="+rfc6238Secret, "issuer=Melody", "period=30", "digits=6") {
        t.Fatalf("unexpected otpauth uri: %s", uri)
    }
}

func TestGenerateRecoveryCodes_UniqueAndFormatted(t *testing.T) {
    codes, codesErr := GenerateRecoveryCodes(8)
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

func TestOtpauthUri_RendersTheResolvedPeriodInSeconds(t *testing.T) {
    uri, uriErr := OtpauthUri("Melody", "alice@example.com", rfc6238Secret, Config{Period: bound.Of(90 * time.Second), Digits: bound.Of(8)})
    if nil != uriErr {
        t.Fatalf("otpauth uri: %v", uriErr)
    }

    if false == containsAll(uri, "period=90&", "digits=8&") {
        t.Fatalf("unexpected otpauth uri: %s", uri)
    }
}

func TestOtpauthUri_AnswersARefusedConfigAsTheError(t *testing.T) {
    _, uriErr := OtpauthUri("Melody", "alice@example.com", rfc6238Secret, Config{Digits: bound.Of(9)})
    expectRefusalNamingField(t, uriErr, "totp.Config.Digits")
}

func TestResolve_TheZeroValueIsTheInteroperableDefault(t *testing.T) {
    resolved, resolveErr := Config{}.Resolve()
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if 30*time.Second != resolved.Period || 6 != resolved.Digits || 1 != resolved.Skew || nil == resolved.Clock {
        t.Fatalf("expected 30s / 6 / 1 / the system clock, got %+v", resolved)
    }
}

func TestResolve_AValueInsideItsRangeIsKept(t *testing.T) {
    resolved, resolveErr := Config{Period: bound.Of(90 * time.Second), Digits: bound.Of(8), Skew: bound.Of(10)}.Resolve()
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if 90*time.Second != resolved.Period || 8 != resolved.Digits || 10 != resolved.Skew {
        t.Fatalf("expected 90s / 8 / 10, got %+v", resolved)
    }
}

func TestResolve_AValueOutsideItsRangeIsRefusedNamingTheField(t *testing.T) {
    for field, config := range map[string]Config{
        "totp.Config.Period": {Period: bound.Of(25 * time.Hour)},
        "totp.Config.Digits": {Digits: bound.Of(9)},
        "totp.Config.Skew":   {Skew: bound.Of(11)},
    } {
        _, resolveErr := config.Resolve()
        expectRefusalNamingField(t, resolveErr, field)
    }

    for field, config := range map[string]Config{
        "totp.Config.Period": {Period: bound.Of(time.Duration(0))},
        "totp.Config.Digits": {Digits: bound.Of(5)},
        "totp.Config.Skew":   {Skew: bound.Of(-1)},
    } {
        _, resolveErr := config.Resolve()
        expectRefusalNamingField(t, resolveErr, field)
    }
}

func TestResolve_UnlimitedIsRefusedOnEveryField(t *testing.T) {
    for field, config := range map[string]Config{
        "totp.Config.Period": {Period: bound.UnlimitedDuration()},
        "totp.Config.Digits": {Digits: bound.UnlimitedCount()},
        "totp.Config.Skew":   {Skew: bound.UnlimitedCount()},
    } {
        _, resolveErr := config.Resolve()
        expectRefusalNamingField(t, resolveErr, field)
    }
}

func TestResolve_APeriodOfAFractionOfASecondIsRefused(t *testing.T) {
    _, resolveErr := Config{Period: bound.Of(1500 * time.Millisecond)}.Resolve()
    expectRefusalNamingField(t, resolveErr, "totp.Config.Period")
}

/* each instant is counter 1 under its period, the counter of the RFC 6238 known answer */
func TestVerify_ANonDefaultPeriodStepsTheCounterByItsSeconds(t *testing.T) {
    for period, at := range map[time.Duration]int64{
        time.Second:      1,
        60 * time.Second: 118,
        24 * time.Hour:   86400,
    } {
        config := Config{Period: bound.Of(period), Skew: bound.Of(0), Clock: clock.NewFrozenClock(time.Unix(at, 0))}

        code, codeErr := GenerateCodeAt(rfc6238Secret, time.Unix(at, 0), config)
        if nil != codeErr || "287082" != code {
            t.Fatalf("expected the known answer for a %v period at T=%d, got %q (%v)", period, at, code, codeErr)
        }

        ok, verifyErr := Verify(rfc6238Secret, "287082", config)
        if nil != verifyErr || false == ok {
            t.Fatalf("expected the known answer to verify for a %v period at T=%d, ok=%v err=%v", period, at, ok, verifyErr)
        }
    }
}

func TestVerify_ASkewOfZeroAcceptsTheCurrentStepAlone(t *testing.T) {
    secret, _ := GenerateSecret()
    now := time.Unix(1_700_000_010, 0)

    current, _ := GenerateCodeAt(secret, now, Config{})
    previous, _ := GenerateCodeAt(secret, now.Add(-30*time.Second), Config{})

    ok, verifyErr := Verify(secret, current, Config{Skew: bound.Of(0), Clock: clock.NewFrozenClock(now)})
    if nil != verifyErr || false == ok {
        t.Fatalf("expected the current step to verify under a zero skew, ok=%v err=%v", ok, verifyErr)
    }

    ok, verifyErr = Verify(secret, previous, Config{Skew: bound.Of(0), Clock: clock.NewFrozenClock(now)})
    if nil != verifyErr || true == ok {
        t.Fatalf("expected the previous step to be refused under a zero skew, ok=%v err=%v", ok, verifyErr)
    }
}

func TestVerify_AnswersARefusedConfigAsTheError(t *testing.T) {
    _, verifyErr := Verify(rfc6238Secret, "287082", Config{Skew: bound.Of(11)})
    expectRefusalNamingField(t, verifyErr, "totp.Config.Skew")
}

func TestGenerateCodeAt_AnswersARefusedConfigAsTheError(t *testing.T) {
    _, codeErr := GenerateCodeAt(rfc6238Secret, time.Unix(59, 0), Config{Period: bound.Of(25 * time.Hour)})
    expectRefusalNamingField(t, codeErr, "totp.Config.Period")
}

func TestVerify_ANilClockReadsTheSystemClock(t *testing.T) {
    secret, _ := GenerateSecret()

    code, _ := GenerateCodeAt(secret, time.Now(), Config{})

    ok, verifyErr := Verify(secret, code, Config{})
    if nil != verifyErr || false == ok {
        t.Fatalf("expected a code for now to verify on the system clock, ok=%v err=%v", ok, verifyErr)
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

func TestVerify_ToleratesWhitespaceInTheSubmittedCode(t *testing.T) {
    at := time.Unix(1111111109, 0)

    code, codeErr := GenerateCodeAt(rfc6238Secret, at, Config{})
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
        valid, verifyErr := Verify(rfc6238Secret, variant, Config{Clock: clock.NewFrozenClock(at)})
        if nil != verifyErr {
            t.Fatalf("unexpected verify error for %q: %v", variant, verifyErr)
        }

        if false == valid {
            t.Fatalf("expected the whitespace variant %q to verify", variant)
        }
    }
}

func TestVerify_StillRejectsWrongAndShortCodes(t *testing.T) {
    at := time.Unix(1111111109, 0)

    code, codeErr := GenerateCodeAt(rfc6238Secret, at, Config{})
    if nil != codeErr {
        t.Fatalf("unexpected code error: %v", codeErr)
    }

    /* flip the last digit so the normalized form is a well-formed but wrong code */
    flipped := code[:5] + string('0'+(code[5]-'0'+1)%10)

    for _, invalid := range []string{"12 34", "  ", flipped[:3] + " " + flipped[3:]} {
        valid, verifyErr := Verify(rfc6238Secret, invalid, Config{Clock: clock.NewFrozenClock(at)})
        if nil != verifyErr {
            t.Fatalf("unexpected verify error for %q: %v", invalid, verifyErr)
        }

        if true == valid {
            t.Fatalf("expected %q to be rejected", invalid)
        }
    }
}

/* NormalizeCode is the single normalization Verify applies; callers that key a replay guard on an accepted code must use it, so it is exported and must stay in step with Verify. */
func TestNormalizeCode_StripsEveryWhitespaceForm(t *testing.T) {
    /* the last variant carries a real non-breaking space (U+00A0), the separator a mobile copy/paste of a displayed code keeps */
    for _, variant := range []string{"123456", "123 456", " 123456 ", "1 2 3 4 5 6", "123\t456", "123 456"} {
        if normalized := NormalizeCode(variant); "123456" != normalized {
            t.Fatalf("expected %q to normalize to \"123456\", got %q", variant, normalized)
        }
    }
}

func expectRefusalNamingField(t *testing.T, err error, field string) {
    t.Helper()

    var refusal *exception.Error
    if false == errors.As(err, &refusal) {
        t.Fatalf("expected a refusal naming %s, got %v", field, err)
    }

    if field != refusal.Context()["field"] {
        t.Fatalf("expected the refusal to name %s, got %+v", field, refusal.Context())
    }
}
