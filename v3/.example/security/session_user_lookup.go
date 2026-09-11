package security

import (
    "crypto/sha256"
    "encoding/hex"

    "github.com/precision-soft/melody/v3/.example/entity"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
)

/* SessionUserLookup reads the current account without a cache. */
type SessionUserLookup func(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error)

/* SessionCredentialVersion binds a server-side session to the password hash verified at login, without storing that hash in the session. */
func SessionCredentialVersion(passwordHash string) string {
    digest := sha256.Sum256([]byte(passwordHash))
    return hex.EncodeToString(digest[:])
}
