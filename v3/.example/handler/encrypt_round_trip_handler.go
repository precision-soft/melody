package handler

import (
    nethttp "net/http"

    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* EncryptRoundTripHandler shows what the configured cipher does to a value, which is the one thing the columns it protects cannot show: the second factor's shared secret is stored encrypted, and looking at that column tells you the encryption happened but not that it is reversible for the right key.

   It reports the ciphertext beside the value that came back, so a cipher that had degraded to a pass-through — the failure that loses every secret in the database while every read still looks correct — is visible here rather than only in a test. */
func EncryptRoundTripHandler(cipher melodyencrypt.Cipher) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        plaintext := "sensitive-value"

        ciphertext, encryptErr := cipher.Encrypt(plaintext)
        if nil != encryptErr {
            return nil, encryptErr
        }

        decrypted, decryptErr := cipher.Decrypt(ciphertext)
        if nil != decryptErr {
            return nil, decryptErr
        }

        return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{
            "plaintext":  plaintext,
            "ciphertext": ciphertext,
            "decrypted":  decrypted,
            "roundTrip":  plaintext == decrypted,
        })
    }
}
