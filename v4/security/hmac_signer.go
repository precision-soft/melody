package security

import (
    "context"
    "crypto/rand"
    "encoding/base64"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* HmacEnvelopeSignerConfig configures the client side of the internal-auth scheme, which a calling service uses to sign a request its callee's HmacTokenSource accepts. */
type HmacEnvelopeSignerConfig struct {
    /* Application is the calling application's own name, recorded in the envelope and matched against the callee's application registry. */
    Application string

    /* HeaderName overrides the header the envelope is written to; defaults to DefaultHmacHeaderName. */
    HeaderName string

    /* Ttl is how long a signed envelope stays valid from its issue instant: 30s by default, at least 1s, unlimited refused, since the callee's nonce guard remembers the nonce for the envelope's life. */
    Ttl bound.Duration

    /* Audience, when set, names the callee service and is signed into the envelope; a callee whose ServiceIdentity differs refuses it. Empty is accepted only by a callee without a ServiceIdentity; a callee that enforces an audience refuses an envelope carrying none. */
    Audience string

    /* MinimumSecretLength is the fewest bytes the current key's secret may hold: 32 by default, the RFC 7518 floor for an HMAC-SHA256 key, at least 32, unlimited refused; the signer refuses to sign with a shorter one, so a custom provider handing a short key signs nothing. */
    MinimumSecretLength bound.Count

    /* Clock is the clock the envelope's issue and expiry instants are stamped from; nil uses the system clock. Inject a frozen clock for deterministic tests. */
    Clock clockcontract.Clock
}

var hmacEnvelopeSignerTtlRule = bound.Rule[time.Duration]{
    Name:    "HmacEnvelopeSignerConfig.Ttl",
    Default: 30 * time.Second,
    Minimum: bound.TtlMinimum,
}

/* NewHmacEnvelopeSigner judges the provider's current key through the context given, whatever the provider: it must exist, be bound to the signer's application and hold at least MinimumSecretLength bytes. The signer is built at boot, under the application's context or, inside a container provider, the one container.ContextOf answers; a nil context, a refusal and a provider that could not answer panic. Sign judges the key again whenever the current key id changes. */
func NewHmacEnvelopeSigner(
    ctx context.Context,
    secrets HmacSecretProvider,
    config HmacEnvelopeSignerConfig,
) *HmacEnvelopeSigner {
    if true == internal.IsNilInterface(ctx) {
        exception.Panic(exception.NewError("hmac signer context is nil", map[string]any{"argument": "ctx"}, nil))
    }

    ttl, ttlErr := bound.Resolve(config.Ttl, hmacEnvelopeSignerTtlRule)
    if nil != ttlErr {
        exception.Panic(exception.FromError(ttlErr))
    }

    minimumSecretLength := resolveMinimumSecretLength(config.MinimumSecretLength, "HmacEnvelopeSignerConfig.MinimumSecretLength")

    if "" == config.Application {
        exception.Panic(exception.NewError("hmac signer application is empty", nil, nil))
    }

    if true == internal.IsNilInterface(secrets) {
        exception.Panic(exception.NewError("hmac signer secrets provider is nil", map[string]any{"argument": "secrets"}, nil))
    }

    headerName := config.HeaderName
    if "" == headerName {
        headerName = DefaultHmacHeaderName
    }

    clockInstance := config.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    signer := &HmacEnvelopeSigner{
        application:         config.Application,
        secrets:             secrets,
        headerName:          headerName,
        ttl:                 ttl.Value,
        minimumSecretLength: minimumSecretLength,
        audience:            config.Audience,
        clock:               clockInstance,
    }

    keyId, keyIdErr := signer.currentKeyId(ctx)
    if nil != keyIdErr {
        exception.Panic(exception.FromError(keyIdErr))
    }

    if boundErr := signer.refuseUnboundKey(ctx, keyId); nil != boundErr {
        exception.Panic(exception.FromError(boundErr))
    }

    if _, secretErr := signer.currentSecret(ctx, keyId); nil != secretErr {
        exception.Panic(exception.FromError(secretErr))
    }

    signer.judgedKeyId = keyId

    return signer
}

type HmacEnvelopeSigner struct {
    application         string
    secrets             HmacSecretProvider
    headerName          string
    ttl                 time.Duration
    minimumSecretLength int
    audience            string
    clock               clockcontract.Clock
    judgedMutex         sync.Mutex
    judgedKeyId         string
}

func (instance *HmacEnvelopeSigner) HeaderName() string {
    return instance.headerName
}

/* Sign builds the internal-auth header value binding the call to method, path, query string and the given body, optionally propagating an originating actor. The path argument may carry a query string (everything after the first '?'); it is signed separately and matched against the request's raw query at the callee. The path is matched at the callee against the spelling its router matched — each segment decoded on its own, a separator encoded inside a segment kept as "%2F", in upper case whatever case the request line used — so a caller signs "/files/café" for a request line "/files/caf%C3%A9" and "/files/a%2Fb" for the one-segment resource "a/b"; a request whose segment decodes to a literal "%2F" cannot be signed and is refused. Since the argument is cut at its first "?", a request whose path segment decodes to a name carrying "?" cannot be signed either: the signature covers the path before it, and the callee refuses the envelope. The provider is asked under the runtime's context. A provider that could not answer is returned marked as an infrastructure failure; a current key the provider rotated to is judged as the constructor judges it. The returned string is written to HeaderName() on the outgoing request. */
func (instance *HmacEnvelopeSigner) Sign(
    runtimeInstance containercontract.Runtime,
    method string,
    path string,
    body []byte,
    actor securitycontract.Actor,
) (string, error) {
    if runtimeErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return "", runtimeErr
    }

    ctx := runtimeInstance.Context()

    keyId, keyIdErr := instance.currentKeyId(ctx)
    if nil != keyIdErr {
        return "", keyIdErr
    }

    if false == instance.keyIdJudged(keyId) {
        if boundErr := instance.refuseUnboundKey(ctx, keyId); nil != boundErr {
            return "", boundErr
        }
    }

    secret, secretErr := instance.currentSecret(ctx, keyId)
    if nil != secretErr {
        return "", secretErr
    }

    instance.markKeyIdJudged(keyId)

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

func (instance *HmacEnvelopeSigner) keyIdJudged(keyId string) bool {
    instance.judgedMutex.Lock()
    defer instance.judgedMutex.Unlock()

    return keyId == instance.judgedKeyId
}

func (instance *HmacEnvelopeSigner) markKeyIdJudged(keyId string) {
    instance.judgedMutex.Lock()
    defer instance.judgedMutex.Unlock()

    instance.judgedKeyId = keyId
}

func (instance *HmacEnvelopeSigner) currentKeyId(ctx context.Context) (string, error) {
    keyId, keyIdErr := instance.secrets.CurrentKeyId(ctx)
    if nil != keyIdErr {
        return "", exception.NewError("hmac signer secret provider failed", nil, exception.MarkInfrastructureFailure(keyIdErr))
    }

    return keyId, nil
}

func (instance *HmacEnvelopeSigner) refuseUnboundKey(ctx context.Context, keyId string) error {
    boundApplication, keyBound, boundErr := instance.secrets.ApplicationForKeyId(ctx, keyId)
    if nil != boundErr {
        return exception.NewError("hmac signer secret provider failed", map[string]any{"keyId": keyId}, exception.MarkInfrastructureFailure(boundErr))
    }

    if false == keyBound || boundApplication != instance.application {
        return exception.NewError(
            "hmac signer current key id is not bound to the signer application",
            map[string]any{"keyId": keyId, "signerApplication": instance.application, "boundApplication": boundApplication},
            nil,
        )
    }

    return nil
}

func (instance *HmacEnvelopeSigner) currentSecret(ctx context.Context, keyId string) ([]byte, error) {
    secret, secretExists, secretErr := instance.secrets.Secret(ctx, keyId)
    if nil != secretErr {
        return nil, exception.NewError("hmac signer secret provider failed", map[string]any{"keyId": keyId}, exception.MarkInfrastructureFailure(secretErr))
    }

    if false == secretExists {
        return nil, exception.NewError(
            "hmac signer has no secret for the current key id",
            map[string]any{"keyId": keyId},
            nil,
        )
    }

    if instance.minimumSecretLength > len(secret) {
        shortSecret := shortSecretContext("NewHmacEnvelopeSigner.secrets", len(secret), instance.minimumSecretLength)
        shortSecret["keyId"] = keyId

        return nil, exception.NewError("hmac signer secret is shorter than its minimum, so it signs nothing", shortSecret, nil)
    }

    return secret, nil
}

func newNonce() (string, error) {
    raw := make([]byte, 16)
    if _, readErr := rand.Read(raw); nil != readErr {
        return "", exception.NewError("could not generate internal-auth nonce", nil, readErr)
    }

    return base64.RawURLEncoding.EncodeToString(raw), nil
}
