package security

import (
    "crypto/sha256"
    "crypto/subtle"
)

/* constantTimeSecretEquals compares a presented secret against the expected one without leaking its length through time: subtle.ConstantTimeCompare answers at once on unequal lengths, so both sides are hashed first and every comparison runs over thirty-two bytes. Both api-key doors, the authenticator and the rule, compare through it. */
func constantTimeSecretEquals(expectedValue string, presentedValue string) bool {
    expectedDigest := sha256.Sum256([]byte(expectedValue))
    presentedDigest := sha256.Sum256([]byte(presentedValue))

    return 1 == subtle.ConstantTimeCompare(expectedDigest[:], presentedDigest[:])
}
