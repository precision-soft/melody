package security

import (
    "crypto/rand"
    "encoding/base64"
    "strings"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* HmacEnvelopeSignerConfig configures the client side of the internal-auth scheme, which a calling service uses to sign a request its callee's HmacTokenSource accepts. */
type HmacEnvelopeSignerConfig struct {
    /* Application is the calling application's own name, recorded in the envelope and matched against the callee's application registry. */
    Application string

    Secrets HmacSecretProvider

    /* HeaderName overrides the header the envelope is written to; defaults to DefaultHmacHeaderName. */
    HeaderName string

    /* Ttl is how long a signed envelope stays valid from its issue instant: 30s by default, at least 1s, unlimited refused, since the callee's nonce guard remembers the nonce for the envelope's life. */
    Ttl bound.Duration

    /* Audience, when set, names the callee service and is signed into the envelope; a callee whose ServiceIdentity differs refuses it. Empty is accepted only by a callee without a ServiceIdentity; a callee that enforces an audience refuses an envelope carrying none. */
    Audience string

    /* MinimumSecretLength is the fewest bytes the current key's secret may hold: 32 by default, the RFC 7518 floor for an HMAC-SHA256 key, at least 32, unlimited refused; Sign refuses to sign with a shorter one, so a custom provider handing a short key signs nothing. */
    MinimumSecretLength bound.Count

    /* Clock is the clock the envelope's issue and expiry instants are stamped from; nil uses the system clock. Inject a frozen clock for deterministic tests. */
    Clock clockcontract.Clock
}

var hmacEnvelopeSignerTtlRule = bound.Rule[time.Duration]{
    Name:    "HmacEnvelopeSignerConfig.Ttl",
    Default: 30 * time.Second,
    Minimum: bound.TtlMinimum,
}

func NewHmacEnvelopeSigner(config HmacEnvelopeSignerConfig) *HmacEnvelopeSigner {
    ttl, ttlErr := bound.Resolve(config.Ttl, hmacEnvelopeSignerTtlRule)
    if nil != ttlErr {
        exception.Panic(exception.FromError(ttlErr))
    }

    minimumSecretLength := resolveMinimumSecretLength(config.MinimumSecretLength, "HmacEnvelopeSignerConfig.MinimumSecretLength")

    if "" == config.Application {
        exception.Panic(exception.NewError("hmac signer application is empty", nil, nil))
    }

    if true == internal.IsNilInterface(config.Secrets) {
        exception.Panic(exception.NewError("hmac signer secrets provider is nil", nil, nil))
    }

    /* the signer's current key must be issued to the application it signs for, since the verifier refuses any other */
    currentKeyId := config.Secrets.CurrentKeyId()
    if boundApplication, keyBound := config.Secrets.ApplicationForKeyId(currentKeyId); false == keyBound || boundApplication != config.Application {
        exception.Panic(
            exception.NewError(
                "hmac signer current key id is not bound to the signer application",
                map[string]any{"keyId": currentKeyId, "signerApplication": config.Application, "boundApplication": boundApplication},
                nil,
            ),
        )
    }

    /* judged again at Sign, for a provider that rotates after boot */
    if currentSecret, secretExists := config.Secrets.Secret(currentKeyId); true == secretExists && minimumSecretLength > len(currentSecret) {
        shortSecret := shortSecretContext("HmacEnvelopeSignerConfig.Secrets", len(currentSecret), minimumSecretLength)
        shortSecret["keyId"] = currentKeyId

        exception.Panic(exception.NewError("hmac signer secret is shorter than its minimum, so it signs nothing", shortSecret, nil))
    }

    headerName := config.HeaderName
    if "" == headerName {
        headerName = DefaultHmacHeaderName
    }

    clockInstance := config.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    return &HmacEnvelopeSigner{
        application:         config.Application,
        secrets:             config.Secrets,
        headerName:          headerName,
        ttl:                 ttl.Value,
        minimumSecretLength: minimumSecretLength,
        audience:            config.Audience,
        clock:               clockInstance,
    }
}

type HmacEnvelopeSigner struct {
    application         string
    secrets             HmacSecretProvider
    headerName          string
    ttl                 time.Duration
    minimumSecretLength int
    audience            string
    clock               clockcontract.Clock
}

func (instance *HmacEnvelopeSigner) HeaderName() string {
    return instance.headerName
}

/* Sign builds the internal-auth header value binding the call to method, path, query string and the given body, optionally propagating an originating actor. The path argument may carry a query string (everything after the first '?'); it is signed separately and matched against the request's raw query at the callee. The path is matched at the callee against the spelling its router matched — each segment decoded on its own, a separator encoded inside a segment kept as "%2F", in upper case whatever case the request line used — so a caller signs "/files/café" for a request line "/files/caf%C3%A9" and "/files/a%2Fb" for the one-segment resource "a/b"; a request whose segment decodes to a literal "%2F" cannot be signed and is refused. Since the argument is cut at its first "?", a request whose path segment decodes to a name carrying "?" cannot be signed either: the signature covers the path before it, and the callee refuses the envelope. The returned string is written to HeaderName() on the outgoing request. */
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

    if instance.minimumSecretLength > len(secret) {
        shortSecret := shortSecretContext("HmacEnvelopeSignerConfig.Secrets", len(secret), instance.minimumSecretLength)
        shortSecret["keyId"] = keyId

        return "", exception.NewError("hmac signer secret is shorter than its minimum, so it signs nothing", shortSecret, nil)
    }

    nonce, nonceErr := newNonce()
    if nil != nonceErr {
        return "", nonceErr
    }

    now := instance.clock.Now()

    signedPath, signedQuery, _ := strings.Cut(path, "?")

    envelope := hmacEnvelope{
        Application: instance.application,
        Audience:    instance.audience,
        Method:      method,
        Path:        signedPath,
        Query:       signedQuery,
        IssuedAt:    now.Unix(),
        ExpiresAt:   now.Add(instance.ttl).Unix(),
        Nonce:       nonce,
        BodyHash:    hashBody(body),
        Actor:       ActorToData(actor),
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
