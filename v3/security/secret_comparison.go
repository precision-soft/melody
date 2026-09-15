package security

import (
    "crypto/sha256"
    "crypto/subtle"
)

func constantTimeSecretEquals(expectedValue string, presentedValue string) bool {
    expectedDigest := sha256.Sum256([]byte(expectedValue))
    presentedDigest := sha256.Sum256([]byte(presentedValue))

    return 1 == subtle.ConstantTimeCompare(expectedDigest[:], presentedDigest[:])
}
