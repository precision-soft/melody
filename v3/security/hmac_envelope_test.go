package security

import (
    "encoding/base64"
    "encoding/json"
    "strings"
    "testing"
)

func hmacEnvelopeProbeSecrets() *StaticHmacSecretProvider {
    return NewStaticHmacSecretProvider("key-current", map[string]HmacKey{
        "key-current": {App: "billing", Secret: []byte("current-shared-secret-value-0001")},
    })
}

func hmacEnvelopeProbe() hmacEnvelope {
    return hmacEnvelope{
        App:       "billing",
        Method:    "GET",
        Path:      "/orders",
        IssuedAt:  1000,
        ExpiresAt: 1030,
        Nonce:     "nonce-1",
        BodyHash:  hashBody(nil),
    }
}

/* an empty body hashes deterministically rather than being skipped: a signature that covered no body for an empty request would let a body be added to a captured envelope */
func TestHashBody_HashesTheEmptyBodyToo(t *testing.T) {
    if "" == hashBody(nil) {
        t.Fatal("expected an empty body to hash to something")
    }

    if hashBody(nil) != hashBody([]byte{}) {
        t.Fatal("expected nil and an empty slice to hash alike")
    }

    if hashBody(nil) == hashBody([]byte("payload")) {
        t.Fatal("expected a body to change the hash")
    }

    if true == strings.ContainsAny(hashBody([]byte("payload")), "+/=") {
        t.Fatalf("expected an unpadded base64url digest, got %q", hashBody([]byte("payload")))
    }
}

func TestEncodeHmacHeaderValue_RoundTripsThroughTheDecoder(t *testing.T) {
    secret, _ := hmacEnvelopeProbeSecrets().Secret("key-current")

    headerValue, encodeErr := encodeHmacHeaderValue("key-current", hmacEnvelopeProbe(), secret)
    if nil != encodeErr {
        t.Fatalf("unexpected encode error: %v", encodeErr)
    }

    if 3 != len(strings.Split(headerValue, ".")) {
        t.Fatalf("expected three dot-separated parts, got %q", headerValue)
    }

    envelope, keyId, decodeErr := decodeHmacHeaderValue(headerValue, hmacEnvelopeProbeSecrets())
    if nil != decodeErr {
        t.Fatalf("unexpected decode error: %v", decodeErr)
    }

    if "key-current" != keyId {
        t.Fatalf("expected the signing key id to be reported, got %q", keyId)
    }

    if hmacEnvelopeProbe() != envelope {
        t.Fatalf("expected the envelope to survive the round trip, got %#v", envelope)
    }
}

/* the decoder fails closed on every structural problem: each of these would otherwise reach the signature comparison or past it with a half-read envelope */
func TestDecodeHmacHeaderValue_RefusesEveryStructuralDamage(t *testing.T) {
    secret, _ := hmacEnvelopeProbeSecrets().Secret("key-current")
    valid, _ := encodeHmacHeaderValue("key-current", hmacEnvelopeProbe(), secret)
    parts := strings.Split(valid, ".")

    for _, probe := range []struct {
        name        string
        headerValue string
        expected    string
    }{
        {name: "two parts", headerValue: parts[0] + "." + parts[1], expected: "internal-auth header has an invalid structure"},
        {name: "four parts", headerValue: valid + ".extra", expected: "internal-auth header has an invalid structure"},
        {name: "header not base64url", headerValue: "not base64!." + parts[1] + "." + parts[2], expected: "internal-auth header is not valid base64url"},
        {name: "header not json", headerValue: base64.RawURLEncoding.EncodeToString([]byte("{")) + "." + parts[1] + "." + parts[2], expected: "internal-auth header is not valid json"},
        {name: "signature not base64url", headerValue: parts[0] + "." + parts[1] + ".not base64!", expected: "internal-auth signature is not valid base64url"},
    } {
        _, _, decodeErr := decodeHmacHeaderValue(probe.headerValue, hmacEnvelopeProbeSecrets())
        if nil == decodeErr || false == strings.Contains(decodeErr.Error(), probe.expected) {
            t.Fatalf("%s: expected %q, got %v", probe.name, probe.expected, decodeErr)
        }
    }
}

/* the algorithm and the type are the domain separation: a JSON web token is byte-identical in shape and signs through the same primitive, so without a type of its own the only thing keeping one credential from verifying as the other is the two secrets happening to differ */
func TestDecodeHmacHeaderValue_RefusesAnyOtherAlgorithmOrType(t *testing.T) {
    secret, _ := hmacEnvelopeProbeSecrets().Secret("key-current")

    for _, probe := range []struct {
        header   hmacEnvelopeHeader
        expected string
    }{
        {header: hmacEnvelopeHeader{Algorithm: "none", Type: hmacEnvelopeType, KeyId: "key-current"}, expected: "internal-auth algorithm is not supported"},
        {header: hmacEnvelopeHeader{Algorithm: "", Type: hmacEnvelopeType, KeyId: "key-current"}, expected: "internal-auth algorithm is not supported"},
        {header: hmacEnvelopeHeader{Algorithm: hmacEnvelopeAlgorithm, Type: "JWT", KeyId: "key-current"}, expected: "internal-auth type is not accepted"},
        {header: hmacEnvelopeHeader{Algorithm: hmacEnvelopeAlgorithm, Type: "", KeyId: "key-current"}, expected: "internal-auth type is not accepted"},
        {header: hmacEnvelopeHeader{Algorithm: hmacEnvelopeAlgorithm, Type: hmacEnvelopeType, KeyId: "key-absent"}, expected: "internal-auth key id is not known"},
    } {
        headerBytes, _ := json.Marshal(probe.header)
        payloadBytes, _ := json.Marshal(hmacEnvelopeProbe())

        part0 := base64.RawURLEncoding.EncodeToString(headerBytes)
        part1 := base64.RawURLEncoding.EncodeToString(payloadBytes)
        part2 := base64.RawURLEncoding.EncodeToString(signHmacSha256(part0+"."+part1, secret))

        _, _, decodeErr := decodeHmacHeaderValue(part0+"."+part1+"."+part2, hmacEnvelopeProbeSecrets())
        if nil == decodeErr || false == strings.Contains(decodeErr.Error(), probe.expected) {
            t.Fatalf("%#v: expected %q, got %v", probe.header, probe.expected, decodeErr)
        }
    }
}

/* the migration window admits exactly the envelopes a signer that predates the typ can mint, and nothing else: an envelope carrying NO typ verifies, while one carrying a WRONG typ is refused whether or not the window is open. Without the second half the window would let a JSON web token through on its type as well as on its absence. */
func TestDecodeHmacHeaderValue_TheMigrationWindowAcceptsAnAbsentTypeAndStillRefusesAWrongOne(t *testing.T) {
    secret, _ := hmacEnvelopeProbeSecrets().Secret("key-current")

    encode := func(headerType string) string {
        headerBytes, _ := json.Marshal(hmacEnvelopeHeader{Algorithm: hmacEnvelopeAlgorithm, Type: headerType, KeyId: "key-current"})
        payloadBytes, _ := json.Marshal(hmacEnvelopeProbe())

        part0 := base64.RawURLEncoding.EncodeToString(headerBytes)
        part1 := base64.RawURLEncoding.EncodeToString(payloadBytes)

        return part0 + "." + part1 + "." + base64.RawURLEncoding.EncodeToString(signHmacSha256(part0+"."+part1, secret))
    }

    envelope, keyId, absentErr := decodeHmacHeaderValueAcceptingUntypedEnvelopes(encode(""), hmacEnvelopeProbeSecrets(), true)
    if nil != absentErr {
        t.Fatalf("expected an envelope minted before the typ to be accepted inside the window, got %v", absentErr)
    }

    if "key-current" != keyId {
        t.Fatalf("expected the key id to be carried through, got %q", keyId)
    }

    if hmacEnvelopeProbe().Path != envelope.Path {
        t.Fatalf("expected the payload to be carried through, got path %q", envelope.Path)
    }

    _, _, wrongErr := decodeHmacHeaderValueAcceptingUntypedEnvelopes(encode("JWT"), hmacEnvelopeProbeSecrets(), true)
    if nil == wrongErr || false == strings.Contains(wrongErr.Error(), "internal-auth type is not accepted") {
        t.Fatalf("expected a wrong typ to be refused even inside the window, got %v", wrongErr)
    }

    _, _, closedErr := decodeHmacHeaderValueAcceptingUntypedEnvelopes(encode(""), hmacEnvelopeProbeSecrets(), false)
    if nil == closedErr || false == strings.Contains(closedErr.Error(), "internal-auth type is not accepted") {
        t.Fatalf("expected an absent typ to be refused with the window closed, got %v", closedErr)
    }
}

func TestDecodeHmacHeaderValue_RefusesASignatureOverOtherContent(t *testing.T) {
    secret, _ := hmacEnvelopeProbeSecrets().Secret("key-current")
    valid, _ := encodeHmacHeaderValue("key-current", hmacEnvelopeProbe(), secret)
    parts := strings.Split(valid, ".")

    tampered := hmacEnvelopeProbe()
    tampered.Path = "/admin"
    tamperedBytes, _ := json.Marshal(tampered)

    _, _, decodeErr := decodeHmacHeaderValue(parts[0]+"."+base64.RawURLEncoding.EncodeToString(tamperedBytes)+"."+parts[2], hmacEnvelopeProbeSecrets())
    if nil == decodeErr || false == strings.Contains(decodeErr.Error(), "internal-auth signature mismatch") {
        t.Fatalf("expected a signature mismatch on a swapped payload, got %v", decodeErr)
    }

    otherSecrets := NewStaticHmacSecretProvider("key-current", map[string]HmacKey{
        "key-current": {App: "billing", Secret: []byte("another-shared-secret-value-002")},
    })

    _, _, otherKeyErr := decodeHmacHeaderValue(valid, otherSecrets)
    if nil == otherKeyErr || false == strings.Contains(otherKeyErr.Error(), "internal-auth signature mismatch") {
        t.Fatalf("expected a signature mismatch under another secret, got %v", otherKeyErr)
    }
}
