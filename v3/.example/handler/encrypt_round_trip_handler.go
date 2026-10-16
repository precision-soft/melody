package handler

import (
    nethttp "net/http"

    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* EncryptRoundTripHandler shows what the configured cipher does to a value: the ciphertext beside the value that comes back, so a cipher that degraded to a pass-through, whose columns still read correctly, is visible here. */
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
