package security

import (
    "crypto/rand"
    "encoding/base64"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

const defaultHmacSignerTtl = 30 * time.Second

/* HmacEnvelopeSignerConfig configures the client side of the internal-auth scheme — the helper a calling service uses to sign an outgoing request so the callee's HmacTokenSource accepts it. Both products share this signer so the canonical envelope form stays identical on both ends. */
type HmacEnvelopeSignerConfig struct {
    /* App is the calling application's own name, recorded in the envelope and matched against the callee's app registry. */
    App string

    Secrets HmacSecretProvider

    /* HeaderName overrides the header the envelope is written to; defaults to DefaultHmacHeaderName. */
    HeaderName string

    /* Ttl is how long a signed envelope stays valid; defaults to defaultHmacSignerTtl. */
    Ttl time.Duration
}

func NewHmacEnvelopeSigner(config HmacEnvelopeSignerConfig) *HmacEnvelopeSigner {
    if "" == config.App {
        exception.Panic(exception.NewError("hmac signer app is empty", nil, nil))
    }

    if true == internal.IsNilInterface(config.Secrets) {
        exception.Panic(exception.NewError("hmac signer secrets provider is nil", nil, nil))
    }

    headerName := config.HeaderName
    if "" == headerName {
        headerName = DefaultHmacHeaderName
    }

    ttl := config.Ttl
    if ttl <= 0 {
        ttl = defaultHmacSignerTtl
    }

    return &HmacEnvelopeSigner{
        app:        config.App,
        secrets:    config.Secrets,
        headerName: headerName,
        ttl:        ttl,
    }
}

type HmacEnvelopeSigner struct {
    app        string
    secrets    HmacSecretProvider
    headerName string
    ttl        time.Duration
}

func (instance *HmacEnvelopeSigner) HeaderName() string {
    return instance.headerName
}

/* Sign builds the internal-auth header value binding the call to method+path and the given body, optionally propagating an originating actor. The returned string is written to HeaderName() on the outgoing request. */
func (instance *HmacEnvelopeSigner) Sign(
    method string,
    path string,
    body []byte,
    actor securitycontract.Actor,
) (string, error) {
    keyId := instance.secrets.CurrentKeyId()
    secret, secretExists := instance.secrets.Secret(keyId)
    if false == secretExists {
        return "", exception.NewError(
            "hmac signer has no secret for the current key id",
            map[string]any{"keyId": keyId},
            nil,
        )
    }

    nonce, nonceErr := newNonce()
    if nil != nonceErr {
        return "", nonceErr
    }

    now := time.Now()

    envelope := hmacEnvelope{
        App:       instance.app,
        Method:    method,
        Path:      path,
        IssuedAt:  now.Unix(),
        ExpiresAt: now.Add(instance.ttl).Unix(),
        Nonce:     nonce,
        BodyHash:  hashBody(body),
        Actor:     ActorToData(actor),
    }

    return encodeHmacHeaderValue(keyId, envelope, secret)
}

func newNonce() (string, error) {
    raw := make([]byte, 16)
    if _, readErr := rand.Read(raw); nil != readErr {
        return "", exception.NewError("could not generate internal-auth nonce", nil, readErr)
    }

    return base64.RawURLEncoding.EncodeToString(raw), nil
}
