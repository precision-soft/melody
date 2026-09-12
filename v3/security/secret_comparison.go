package security

import (
    "crypto/sha256"
    "crypto/subtle"
)

/* constantTimeSecretEquals compares a presented secret against the expected one without leaking anything through time — the length included. subtle.ConstantTimeCompare is constant-time only over inputs of EQUAL length and answers immediately on unequal ones, so the comparison's duration told a caller when a guess had the right length, shrinking the search space to strings of one size. Hashing both sides first makes every comparison run over the same thirty-two bytes whatever the inputs measure, and the digests preserve exactly the equality being asked. Both api-key doors — the authenticator and the rule — compare through this one spelling, so the two cannot drift into disagreeing timing shapes. */
func constantTimeSecretEquals(expectedValue string, presentedValue string) bool {
    expectedDigest := sha256.Sum256([]byte(expectedValue))
    presentedDigest := sha256.Sum256([]byte(presentedValue))

    return 1 == subtle.ConstantTimeCompare(expectedDigest[:], presentedDigest[:])
}
