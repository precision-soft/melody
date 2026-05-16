package security

import (
    "crypto/sha256"
    "encoding/hex"
)

func Sha256Hex(value string) string {
    hash := sha256.Sum256([]byte(value))
    return hex.EncodeToString(hash[:])
}
