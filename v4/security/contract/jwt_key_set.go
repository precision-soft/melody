package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* JwtKeyQuery is what a token names of the key it was signed with: its header's kid, "" when absent, and its alg, already one the validator implements. */
type JwtKeyQuery struct {
    KeyId     string
    Algorithm string
}

/* JwtKey is one verification key: the key id it answers, the algorithm it verifies and the secret of an HMAC algorithm. */
type JwtKey struct {
    KeyId     string
    Algorithm string
    Secret    []byte
}

/* JwtKeySet supplies the keys a token is verified with, under the request's runtime. No key says the token's key is not known and refuses the token; an error says the set could not be asked, and the validator answers it as an infrastructure failure. Several keys are tried in the order answered, each only under the algorithm it names. */
type JwtKeySet interface {
    VerificationKeys(runtimeInstance containercontract.Runtime, query JwtKeyQuery) ([]JwtKey, error)
}
