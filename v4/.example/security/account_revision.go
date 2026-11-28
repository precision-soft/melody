package security

import (
    "crypto/sha256"
    "encoding/hex"
    "slices"
)

/* AccountRevision digests what an open stream's authority rests on: the account, the password version it signed in with and its roles, in any order; a change to any of them gives another revision. */
func AccountRevision(userId string, credentialVersion string, roles []string) string {
    sortedRoles := slices.Clone(roles)
    slices.Sort(sortedRoles)

    digest := sha256.New()
    for _, part := range append([]string{userId, credentialVersion}, sortedRoles...) {
        digest.Write([]byte(part))
        /* the separator keeps two parts from reading as one: "ab"+"c" and "a"+"bc" digest apart */
        digest.Write([]byte{0})
    }

    return hex.EncodeToString(digest.Sum(nil))
}
