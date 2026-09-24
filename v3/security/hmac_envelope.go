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

    /* hmacEnvelopeType is the header typ every envelope is signed under and the only one the decoder accepts: it separates the envelope from the JSON web token, which has the same shape and signs through the same primitive */
    hmacEnvelopeType = "melody-internal"

    /* DefaultHmacHeaderName is the request header the internal-auth envelope is carried on unless a source/signer overrides it. */
    DefaultHmacHeaderName = "X-Melody-Internal-Auth"
)

/* hmacEnvelope is the signed payload of the internal-auth header, one signature covering every field: Method, Path and Query bind it to one endpoint, Audience optionally to one callee, IssuedAt, ExpiresAt and Nonce bound its lifetime and single use, BodyHash covers the body, and Actor carries the originating actor. */
type hmacEnvelope struct {
    App       string                      `json:"app"`
    Audience  string                      `json:"audience,omitempty"`
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
    Type      string `json:"typ"`
    KeyId     string `json:"kid"`
}

/* hashBody returns the base64url-encoded SHA-256 of the request body, so an empty body hashes deterministically rather than being skipped. */
func hashBody(body []byte) string {
    sum := sha256.Sum256(body)

    return base64.RawURLEncoding.EncodeToString(sum[:])
}

func encodeHmacHeaderValue(keyId string, envelope hmacEnvelope, secret []byte) (string, error) {
    headerBytes, headerErr := json.Marshal(hmacEnvelopeHeader{Algorithm: hmacEnvelopeAlgorithm, Type: hmacEnvelopeType, KeyId: keyId})
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

/* decodeHmacHeaderValue verifies the signature against the secret resolved for the envelope's key id and returns the decoded envelope together with the verified key id, so the caller can check the key id is authorized for the envelope's claimed app. It fails closed on any structural, key-lookup or signature problem. It requires the envelope's own typ, which is the strict reading every caller gets unless it asks for the migration window below. */
func decodeHmacHeaderValue(headerValue string, secrets HmacSecretProvider) (hmacEnvelope, string, error) {
    return decodeHmacHeaderValueAcceptingUntypedEnvelopes(headerValue, secrets, false)
}

/* decodeHmacHeaderValueAcceptingUntypedEnvelopes is the same reading, except that an envelope carrying no typ is accepted for the migration window; a present, wrong typ is still refused. */
func decodeHmacHeaderValueAcceptingUntypedEnvelopes(headerValue string, secrets HmacSecretProvider, acceptUntypedEnvelopes bool) (hmacEnvelope, string, error) {
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

    /* the typ is required: a JSON web token carries none, so it is refused on this header even under a secret provider that resolves the empty key id */
    if hmacEnvelopeType != header.Type && false == (true == acceptUntypedEnvelopes && "" == header.Type) {
        return hmacEnvelope{}, "", exception.NewError(
            "internal-auth type is not accepted",
            map[string]any{"type": header.Type},
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
