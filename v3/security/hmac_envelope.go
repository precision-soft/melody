package security

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

const (
    hmacEnvelopeAlgorithm = "HS256"

    /* DefaultHmacHeaderName is the request header the internal-auth envelope is carried on unless a source/signer overrides it. */
    DefaultHmacHeaderName = "X-Melody-Internal-Auth"
)

/* hmacEnvelope is the signed payload of the internal-auth header. Every field that matters for authorization is inside the envelope so the single HMAC signature covers all of it — there is no separate-header canonicalization to get wrong. Method/Path/Query bind the envelope to one endpoint (a captured envelope cannot be replayed against another route, nor have its query parameters tampered with), IssuedAt/ExpiresAt/Nonce bound its lifetime and single use, BodyHash makes the request body tamper-evident, and Actor optionally carries the originating actor (F1) so the callee authorizes/audits as the upstream principal. */
type hmacEnvelope struct {
    App       string                      `json:"app"`
    Method    string                      `json:"method"`
    Path      string                      `json:"path"`
    Query     string                      `json:"query,omitempty"`
    IssuedAt  int64                       `json:"iat"`
    ExpiresAt int64                       `json:"exp"`
    Nonce     string                      `json:"nonce"`
    BodyHash  string                      `json:"bodyHash"`
    Actor     *securitycontract.ActorData `json:"actor,omitempty"`
}

type hmacEnvelopeHeader struct {
    Algorithm string `json:"alg"`
    KeyId     string `json:"kid"`
}

/* hashBody returns the base64url-encoded SHA-256 of the request body, so an empty body hashes deterministically rather than being skipped. */
func hashBody(body []byte) string {
    sum := sha256.Sum256(body)

    return base64.RawURLEncoding.EncodeToString(sum[:])
}

func encodeHmacHeaderValue(keyId string, envelope hmacEnvelope, secret []byte) (string, error) {
    headerBytes, headerErr := json.Marshal(hmacEnvelopeHeader{Algorithm: hmacEnvelopeAlgorithm, KeyId: keyId})
    if nil != headerErr {
        return "", exception.NewError("could not encode internal-auth header", nil, headerErr)
    }

    payloadBytes, payloadErr := json.Marshal(envelope)
    if nil != payloadErr {
        return "", exception.NewError("could not encode internal-auth envelope", nil, payloadErr)
    }

    part0 := base64.RawURLEncoding.EncodeToString(headerBytes)
    part1 := base64.RawURLEncoding.EncodeToString(payloadBytes)
    signature := signHmacSha256(part0+"."+part1, secret)
    part2 := base64.RawURLEncoding.EncodeToString(signature)

    return part0 + "." + part1 + "." + part2, nil
}

/* decodeHmacHeaderValue verifies the signature against the secret resolved for the envelope's key id and returns the decoded envelope together with the verified key id, so the caller can check the key id is authorized for the envelope's claimed app. It fails closed on any structural, key-lookup or signature problem. */
func decodeHmacHeaderValue(headerValue string, secrets HmacSecretProvider) (hmacEnvelope, string, error) {
    parts := strings.Split(headerValue, ".")
    if 3 != len(parts) {
        return hmacEnvelope{}, "", exception.NewError("internal-auth header has an invalid structure", nil, nil)
    }

    headerBytes, headerErr := base64.RawURLEncoding.DecodeString(parts[0])
    if nil != headerErr {
        return hmacEnvelope{}, "", exception.NewError("internal-auth header is not valid base64url", nil, headerErr)
    }

    var header hmacEnvelopeHeader
    if unmarshalErr := json.Unmarshal(headerBytes, &header); nil != unmarshalErr {
        return hmacEnvelope{}, "", exception.NewError("internal-auth header is not valid json", nil, unmarshalErr)
    }

    if hmacEnvelopeAlgorithm != header.Algorithm {
        return hmacEnvelope{}, "", exception.NewError(
            "internal-auth algorithm is not supported",
            map[string]any{"algorithm": header.Algorithm},
            nil,
        )
    }

    secret, secretExists := secrets.Secret(header.KeyId)
    if false == secretExists {
        return hmacEnvelope{}, "", exception.NewError(
            "internal-auth key id is not known",
            map[string]any{"keyId": header.KeyId},
            nil,
        )
    }

    signature, signatureErr := base64.RawURLEncoding.DecodeString(parts[2])
    if nil != signatureErr {
        return hmacEnvelope{}, "", exception.NewError("internal-auth signature is not valid base64url", nil, signatureErr)
    }

    expectedSignature := signHmacSha256(parts[0]+"."+parts[1], secret)
    if false == hmac.Equal(signature, expectedSignature) {
        return hmacEnvelope{}, "", exception.NewError("internal-auth signature mismatch", nil, nil)
    }

    payloadBytes, payloadErr := base64.RawURLEncoding.DecodeString(parts[1])
    if nil != payloadErr {
        return hmacEnvelope{}, "", exception.NewError("internal-auth envelope is not valid base64url", nil, payloadErr)
    }

    var envelope hmacEnvelope
    if unmarshalErr := json.Unmarshal(payloadBytes, &envelope); nil != unmarshalErr {
        return hmacEnvelope{}, "", exception.NewError("internal-auth envelope is not valid json", nil, unmarshalErr)
    }

    return envelope, header.KeyId, nil
}
