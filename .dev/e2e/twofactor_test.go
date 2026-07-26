package main

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/security/totp"
)

/* twoFactorTestSecret is a valid base32 TOTP secret; the codes it produces are deterministic for a given instant,
which is what lets these tests reason about the window without a live enrollment. */
const twoFactorTestSecret = "MAVSJVJQLMEMCEVAMYS3AWZXETSUA3EI"

/* The expired-code helper is the only piece of the two-factor section with logic of its own, and its failure mode
is silent in both directions: a distance shorter than the verification window would make the section assert that a
VALID code is refused (a red run with no defect), and a code that collided with the current one would do the same. */
func TestTwoFactorExpiredCodeAt_ReachesOutsideTheVerificationWindow(t *testing.T) {
    reference := time.Unix(1785000000, 0)

    expired, current, periodsBack, computeErr := twoFactorExpiredCodeAt(twoFactorTestSecret, reference)
    if nil != computeErr {
        t.Fatalf("compute an expired code: %v", computeErr)
    }

    if expired == current {
        t.Fatal("the expired code must differ from the current one, or the section asserts that a valid code is refused")
    }

    resolved := totp.Config{}.Resolve()
    if periodsBack <= int(resolved.Skew) {
        t.Fatalf(
            "the code was taken %d period(s) back while the verification window reaches %d — it is still inside the window",
            periodsBack,
            resolved.Skew,
        )
    }

    /* the decisive assertion: the framework's own verifier must refuse it at the reference instant */
    verified, verifyErr := totp.VerifyAt(twoFactorTestSecret, expired, reference, totp.Config{})
    if nil != verifyErr {
        t.Fatalf("verify the expired code: %v", verifyErr)
    }
    if true == verified {
        t.Fatal("the framework's own verifier still accepts the \"expired\" code, so the section's negative is vacuous")
    }
}

/* the current code has to be accepted at the same instant, or the test above would pass for a broken secret rather
than for a bounded window. */
func TestTwoFactorExpiredCodeAt_TheCurrentCodeIsStillAccepted(t *testing.T) {
    reference := time.Unix(1785000000, 0)

    _, current, _, computeErr := twoFactorExpiredCodeAt(twoFactorTestSecret, reference)
    if nil != computeErr {
        t.Fatalf("compute an expired code: %v", computeErr)
    }

    verified, verifyErr := totp.VerifyAt(twoFactorTestSecret, current, reference, totp.Config{})
    if nil != verifyErr {
        t.Fatalf("verify the current code: %v", verifyErr)
    }
    if false == verified {
        t.Fatal("the code the helper reports as current is not accepted at the reference instant")
    }
}

func TestTwoFactorExpiredCodeAt_ReportsAnUnusableSecret(t *testing.T) {
    if _, _, _, computeErr := twoFactorExpiredCodeAt("not base32 at all!!", time.Now()); nil == computeErr {
        t.Fatal("an unusable secret must be reported rather than silently producing a code")
    }
}

/* the whitespace replay proves the nonce key is normalised, so the spaced form has to be the SAME code with a
space in it — a helper that returned something else would be probing a different code entirely and the negative
would pass for the wrong reason. */
func TestTwoFactorSpacedCode_SplitsTheSameCode(t *testing.T) {
    spaced := twoFactorSpacedCode("409643")

    if "409 643" != spaced {
        t.Fatalf("the spaced form is %q, wanted %q", spaced, "409 643")
    }
    if "409643" != totp.NormalizeCode(spaced) {
        t.Fatalf("the spaced form normalises to %q, wanted the original code", totp.NormalizeCode(spaced))
    }
}

func TestTwoFactorSpacedCode_LeavesAnUnexpectedLengthAlone(t *testing.T) {
    if spaced := twoFactorSpacedCode("1234"); "1234" != spaced {
        t.Fatalf("a code of an unexpected length came back as %q", spaced)
    }
}
