package security

import (
    "crypto/sha256"
    "encoding/hex"

    "github.com/precision-soft/melody/v4/.example/entity"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
)

/* SessionUserLookup reads the account a session names from the source of truth, never from a cache: the session's authority is the account's current roles, and a cached copy would keep a demoted, deleted or re-passworded account's old authority alive. */
type SessionUserLookup func(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error)

/* SessionCredentialVersion binds a session to the password hash verified at login without storing the hash in the session: a password change gives the account another version, and every session written under the one it replaced is refused. */
func SessionCredentialVersion(passwordHash string) string {
    digest := sha256.Sum256([]byte(passwordHash))

    return hex.EncodeToString(digest[:])
}
