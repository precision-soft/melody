package security

import (
    "bytes"
    "io"
    nethttp "net/http"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

const hmacTokenSourceName = "hmacInternal"

/* HmacVerifyBodyBeforeNonceAttribute is the request attribute (settable per route or, on demand, through SetHmacVerifyBodyBeforeNonce) that overrides the source's VerifyBodyBeforeNonce default for a single request. Its value must be a bool; a value of any other type is ignored and the source's default applies. */
const HmacVerifyBodyBeforeNonceAttribute = "security.hmac.verifyBodyBeforeNonce"

/* HmacTokenSourceConfig configures the verifying side of the internal-auth scheme. Secrets resolves the shared key (supporting rotation through multiple resolvable key ids); Applications maps a verified caller to the roles its service principal receives; NonceGuard rejects replayed envelopes (defaults to an in-process guard — supply a shared one for multi-instance deployments); MaxFutureExpiry bounds how far ahead an envelope's expiry may sit. */
type HmacTokenSourceConfig struct {
    Secrets      HmacSecretProvider
    Applications HmacApplicationRegistry
    NonceGuard   securitycontract.NonceGuard
    HeaderName   string

    /* Leeway tolerates clock skew on the issued and expiry checks, and the issued check one second more, the resolution of the IssuedAt claim: 5s when left out, bound.Of(time.Duration(0)) for strict, at most 5m, unlimited refused. The nonce guard remembers a nonce for the leeway too, since an envelope past its expiry by less than the leeway is still honoured and must still be refused as a replay. */
    Leeway bound.Duration

    /* MaxFutureExpiry caps how far past the verifier's clock an envelope's ExpiresAt may sit, since the nonce guard remembers each nonce until its envelope expires and the Leeway passes: 1h by default, at least 1s, unlimited refused, so a signer cannot make the guard hold a nonce for as long as it likes. */
    MaxFutureExpiry bound.Duration

    /* MinimumSecretLength is the fewest bytes a secret the Secrets provider resolves may hold: 32 by default, the RFC 7518 floor for an HMAC-SHA256 key, at least 32, unlimited refused. A shorter secret answers the unavailable token, its cause an infrastructure failure naming the key id that the resolution listener files at Error, so a custom provider handing a short key verifies nothing with it; the static provider refuses one when it is built. */
    MinimumSecretLength bound.Count

    /* ServiceIdentity, when set, turns on audience enforcement: an envelope whose signed Audience differs is refused. Empty skips the audience check. */
    ServiceIdentity string

    /* VerifyBodyBeforeNonce selects the default order of the body and nonce checks. When false (the default) the nonce is consumed before the body is read, so a captured valid envelope can force at most one body buffering — but an on-path party who replays the header with a mutated body burns the nonce and fails the legitimate request as a replay. When true the body hash is verified first, so a body mismatch is rejected without consuming the nonce, at the cost of letting a captured envelope force body buffering until it expires. A per-request override (route attribute HmacVerifyBodyBeforeNonceAttribute, or SetHmacVerifyBodyBeforeNonce) takes precedence for routes/calls that need the opposite trade-off. */
    VerifyBodyBeforeNonce bool

    /* Clock is the clock the envelope's time window and the nonce ttl are read from; nil uses the system clock. Inject a frozen clock for deterministic tests. */
    Clock clockcontract.Clock
}

var hmacTokenSourceLeewayRule = bound.Rule[time.Duration]{
    Name:    "HmacTokenSourceConfig.Leeway",
    Default: defaultLeeway,
    Maximum: maximumLeeway,
}

var hmacTokenSourceMaxFutureExpiryRule = bound.Rule[time.Duration]{
    Name:    "HmacTokenSourceConfig.MaxFutureExpiry",
    Default: time.Hour,
    Minimum: bound.TtlMinimum,
}

func NewHmacTokenSource(config HmacTokenSourceConfig) *HmacTokenSource {
    if true == internal.IsNilInterface(config.Secrets) {
        exception.Panic(exception.NewError("hmac token source secrets provider is nil", nil, nil))
    }

    if true == internal.IsNilInterface(config.Applications) {
        exception.Panic(exception.NewError("hmac token source application registry is nil", nil, nil))
    }

    headerName := config.HeaderName
    if "" == headerName {
        headerName = DefaultHmacHeaderName
    }

    clockInstance := config.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    var nonceGuard securitycontract.NonceGuard = config.NonceGuard
    if true == internal.IsNilInterface(nonceGuard) {
        nonceGuard = NewMemoryNonceGuard(MemoryNonceGuardConfig{Clock: clockInstance})
    }

    leeway, leewayErr := bound.Resolve(config.Leeway, hmacTokenSourceLeewayRule)
    if nil != leewayErr {
        exception.Panic(exception.FromError(leewayErr))
    }

    minimumSecretLength := resolveMinimumSecretLength(config.MinimumSecretLength, "HmacTokenSourceConfig.MinimumSecretLength")

    maxFutureExpiry, maxFutureExpiryErr := bound.Resolve(config.MaxFutureExpiry, hmacTokenSourceMaxFutureExpiryRule)
    if nil != maxFutureExpiryErr {
        exception.Panic(exception.FromError(maxFutureExpiryErr))
    }

    return &HmacTokenSource{
        secrets:               config.Secrets,
        applications:          config.Applications,
        nonceGuard:            nonceGuard,
        headerName:            headerName,
        leeway:                leeway.Value,
        maxFutureExpiry:       maxFutureExpiry.Value,
        minimumSecretLength:   minimumSecretLength,
        verifyBodyBeforeNonce: config.VerifyBodyBeforeNonce,
        serviceIdentity:       config.ServiceIdentity,
        clock:                 clockInstance,
    }
}

/* SetHmacVerifyBodyBeforeNonce overrides, for the given request only, whether the HMAC source verifies the body before consuming the nonce. An application calls it on demand (for example in a route-scoped middleware that runs before the firewall) to flip the configured default for chosen routes or calls; setting the HmacVerifyBodyBeforeNonceAttribute route attribute has the same effect. */
func SetHmacVerifyBodyBeforeNonce(request httpcontract.Request, value bool) {
    request.Attributes().Set(HmacVerifyBodyBeforeNonceAttribute, value)
}

type HmacTokenSource struct {
    secrets               HmacSecretProvider
    applications          HmacApplicationRegistry
    nonceGuard            securitycontract.NonceGuard
    headerName            string
    leeway                time.Duration
    maxFutureExpiry       time.Duration
    minimumSecretLength   int
    verifyBodyBeforeNonce bool
    serviceIdentity       string
    clock                 clockcontract.Clock
}

func (instance *HmacTokenSource) Name() string {
    return hmacTokenSourceName
}

func (instance *HmacTokenSource) Resolve(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
) (securitycontract.Token, error) {
    headerValue := request.Header(instance.headerName)
    if "" == headerValue {
        return NewAnonymousToken(), nil
    }

    envelope, keyId, decodeErr := decodeHmacHeaderValue(headerValue, instance.secrets, instance.minimumSecretLength)
    if nil != decodeErr {
        return instance.reject(runtimeInstance, decodeErr)
    }

    /* the key id must be issued to the application the envelope claims, or any holder of a valid secret could impersonate another application */
    boundApplication, keyBound := instance.secrets.ApplicationForKeyId(keyId)
    if false == keyBound || boundApplication != envelope.Application {
        return instance.reject(
            runtimeInstance,
            exception.NewError(
                "internal-auth key id is not authorized for the claimed application",
                map[string]any{"keyId": internal.BoundDiagnosticText(keyId), "claimedApplication": internal.BoundDiagnosticText(envelope.Application), "boundApplication": boundApplication},
                nil,
            ),
        )
    }

    roles, applicationKnown := instance.applications.RolesForApplication(envelope.Application)
    if false == applicationKnown {
        return instance.reject(
            runtimeInstance,
            exception.NewError("internal-auth application is not known", map[string]any{"application": internal.BoundDiagnosticText(envelope.Application)}, nil),
        )
    }

    if audienceErr := instance.verifyAudience(envelope); nil != audienceErr {
        return instance.reject(runtimeInstance, audienceErr)
    }

    if bindErr := instance.verifyEndpoint(envelope, request); nil != bindErr {
        return instance.reject(runtimeInstance, bindErr)
    }

    if timeErr := instance.verifyTimeWindow(envelope, instance.clock.Now()); nil != timeErr {
        return instance.reject(runtimeInstance, timeErr)
    }

    if checkErr := instance.guardNonceAndBody(runtimeInstance, request, keyId, envelope); nil != checkErr {
        return instance.reject(runtimeInstance, checkErr)
    }

    var actor securitycontract.Actor
    if rebuilt := NewActorFromData(envelope.Actor); nil != rebuilt {
        actor = rebuilt
    }

    return NewAuthenticatedToken(envelope.Application, roles, actor), nil
}

func (instance *HmacTokenSource) verifyAudience(envelope hmacEnvelope) error {
    if "" == instance.serviceIdentity {
        /* audience enforcement is opt-in: with no ServiceIdentity the audience is not checked */
        return nil
    }

    if envelope.Audience != instance.serviceIdentity {
        return exception.NewError(
            "internal-auth audience does not match this service",
            map[string]any{"signed": envelope.Audience, "service": instance.serviceIdentity},
            nil,
        )
    }

    return nil
}

func (instance *HmacTokenSource) verifyEndpoint(envelope hmacEnvelope, request httpcontract.Request) error {
    httpRequest := request.HttpRequest()
    if nil == httpRequest {
        return exception.NewError("internal-auth request is nil", nil, nil)
    }

    if nil == httpRequest.URL {
        /* a request built by hand may carry a nil URL; endpoint verification fails closed on it */
        return exception.NewError("internal-auth request url is nil", nil, nil)
    }

    if envelope.Method != httpRequest.Method {
        return exception.NewError(
            "internal-auth method does not match the request",
            map[string]any{"signed": envelope.Method, "request": httpRequest.Method},
            nil,
        )
    }

    /* the signed path is bound to the spelling the router matched, not to the decoded URL.Path, so an envelope signed for the two-segment "/files/a/b" does not authenticate the one-segment resource "/files/a%2Fb" */
    requestPath := http.RequestPathAsRouted(internal.RequestPathAsSent(httpRequest.URL))

    /* a segment that decodes to a literal "%2F" shares its routed spelling with a separator encoded inside a segment, so the envelope cannot say which of the two resources it names: refused */
    if true == internal.RequestPathCarriesLiteralEncodedSeparator(httpRequest.URL) {
        return exception.NewError(
            "internal-auth path carries a literal encoded separator the signed path cannot name",
            map[string]any{"request": requestPath},
            nil,
        )
    }

    if envelope.Path != requestPath {
        return exception.NewError(
            "internal-auth path does not match the request",
            map[string]any{"signed": envelope.Path, "request": requestPath},
            nil,
        )
    }

    if envelope.Query != httpRequest.URL.RawQuery {
        /* only the parameter names reach the log context; the values may be credentials */
        return exception.NewError(
            "internal-auth query does not match the request",
            map[string]any{
                "signed":  internal.RedactQueryValuesForDiagnostics(envelope.Query),
                "request": internal.RedactQueryValuesForDiagnostics(httpRequest.URL.RawQuery),
            },
            nil,
        )
    }

    return nil
}

func (instance *HmacTokenSource) verifyTimeWindow(envelope hmacEnvelope, now time.Time) error {
    if 0 >= envelope.ExpiresAt {
        return exception.NewError("internal-auth envelope is missing an expiry", nil, nil)
    }

    expiry := time.Unix(envelope.ExpiresAt, 0)

    /* the nonce guard remembers the nonce until exp+leeway, so an unbounded expiry would pin memory */
    if true == expiry.After(now.Add(instance.maxFutureExpiry)) {
        return exception.NewError("internal-auth envelope expiry is too far in the future", nil, nil)
    }

    deadline := expiry.Add(instance.leeway)
    if true == now.After(deadline) {
        return exception.NewError("internal-auth envelope is expired", nil, nil)
    }

    if 0 < envelope.IssuedAt {
        /* IssuedAt carries whole seconds, the signer's instant truncated down, so a receiver whose clock trails the sender's by a millisecond across a second boundary reads the envelope as issued in its future: the claim's own resolution is tolerated on top of the leeway, on this check alone, since the expiry and the nonce ttl are not widened by it */
        activation := time.Unix(envelope.IssuedAt, 0).Add(-instance.leeway).Add(-hmacIssuedAtResolution)
        if true == now.Before(activation) {
            return exception.NewError("internal-auth envelope is not yet valid", nil, nil)
        }
    }

    return nil
}

/* hmacIssuedAtResolution is the resolution of an envelope's IssuedAt claim, a count of whole seconds */
const hmacIssuedAtResolution = time.Second

func (instance *HmacTokenSource) verifyBody(envelope hmacEnvelope, request httpcontract.Request) error {
    httpRequest := request.HttpRequest()
    if nil == httpRequest {
        return exception.NewError("internal-auth request is nil", nil, nil)
    }

    bodyBytes, readErr := readAndRestoreBody(httpRequest)
    if nil != readErr {
        return readErr
    }

    if hashBody(bodyBytes) != envelope.BodyHash {
        return exception.NewError("internal-auth body hash does not match the request body", nil, nil)
    }

    return nil
}

func (instance *HmacTokenSource) guardNonce(runtimeInstance runtimecontract.Runtime, keyId string, envelope hmacEnvelope) error {
    if "" == envelope.Nonce {
        return exception.NewError("internal-auth envelope is missing a nonce", nil, nil)
    }

    ttl := time.Unix(envelope.ExpiresAt, 0).Add(instance.leeway).Sub(instance.clock.Now())
    if securitycontract.NonceTtlRule.Minimum > ttl {
        /* the nonce guard refuses a ttl under its minimum, so an envelope at the edge of the window is refused here, as the envelope's failure rather than the guard's */
        return exception.NewError("internal-auth envelope is too close to expiry to guard against replay", nil, nil)
    }

    seen, rememberErr := instance.nonceGuard.Remember(runtimeInstance, hmacNonceGuardKey(keyId, envelope.Nonce), bound.Of(ttl))
    if nil != rememberErr {
        /* the guard failing to answer is the platform's failure, marked so reject logs it as an incident */
        return exception.NewError("internal-auth nonce guard failed", nil, exception.MarkInfrastructureFailure(rememberErr))
    }

    if true == seen {
        return exception.NewError("internal-auth nonce has already been used", nil, nil)
    }

    return nil
}

/* hmacNonceGuardKey namespaces the caller-chosen envelope nonce before it reaches the shared NonceGuard. The nonce field is fully controlled by whoever signs the envelope, so recording it verbatim would let a holder of a valid key write into any other component's key space of the same guard — for example the TOTP replay guard, which keys "2fa:<user>:<code>". The key id's length is encoded in front of it because key ids have no charset restriction: with a plain "hmac:<keyId>:<nonce>" join, key ids "a" and "a:b" would let a holder of key "a" sign nonce "b:<n>" and pre-burn key "a:b"'s nonce "<n>", forcing rejection of that key's legitimate requests. */
func hmacNonceGuardKey(keyId string, nonce string) string {
    return "hmac:" + strconv.Itoa(len(keyId)) + ":" + keyId + ":" + nonce
}

/* guardNonceAndBody runs the replay and body-hash checks in the order selected for this request. Nonce-first (the default) lets a captured-but-valid envelope force at most one body buffering, since a replay is rejected before readAndRestoreBody runs; body-first rejects a mismatched body without consuming the nonce, so an on-path party cannot burn a legitimate request's nonce by replaying its header with a mutated body. The signature has already been verified, so only a legitimate envelope ever reaches either check. */
func (instance *HmacTokenSource) guardNonceAndBody(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    keyId string,
    envelope hmacEnvelope,
) error {
    if true == instance.effectiveVerifyBodyBeforeNonce(request) {
        if bodyErr := instance.verifyBody(envelope, request); nil != bodyErr {
            return bodyErr
        }

        return instance.guardNonce(runtimeInstance, keyId, envelope)
    }

    if replayErr := instance.guardNonce(runtimeInstance, keyId, envelope); nil != replayErr {
        return replayErr
    }

    return instance.verifyBody(envelope, request)
}

/* effectiveVerifyBodyBeforeNonce resolves the per-request override (a bool under HmacVerifyBodyBeforeNonceAttribute, set by a route attribute or SetHmacVerifyBodyBeforeNonce) and falls back to the source's configured default. */
func (instance *HmacTokenSource) effectiveVerifyBodyBeforeNonce(request httpcontract.Request) bool {
    if override, exists := request.Attributes().Get(HmacVerifyBodyBeforeNonceAttribute); true == exists {
        if value, isBool := override.(bool); true == isBool {
            return value
        }
    }

    return instance.verifyBodyBeforeNonce
}

func (instance *HmacTokenSource) reject(
    runtimeInstance runtimecontract.Runtime,
    cause error,
) (securitycontract.Token, error) {
    /* a guard or a secret provider that cannot answer is the unavailable token, recorded once by the resolution listener; a bad envelope is anonymous */
    if true == exception.IsInfrastructureFailure(cause) {
        return NewUnavailableToken(cause), nil
    }

    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil != logger {
        logger.Info("internal-auth envelope rejected", exception.LogContext(cause, exception.LogContextConfig{}))
    }

    return NewAnonymousToken(), nil
}

/* readAndRestoreBody reads the full request body so it can be hashed, then replaces the consumed body (and GetBody) with a fresh reader so the downstream handler still sees it. Reading happens only after the envelope signature has already been verified, so an unauthenticated caller can never make the server buffer a body. */
func readAndRestoreBody(httpRequest *nethttp.Request) ([]byte, error) {
    if nil == httpRequest.Body {
        return []byte{}, nil
    }

    bodyBytes, readErr := io.ReadAll(httpRequest.Body)
    if nil != readErr {
        return nil, exception.NewError("could not read the request body for internal-auth", nil, readErr)
    }

    _ = httpRequest.Body.Close()

    httpRequest.Body = io.NopCloser(bytes.NewReader(bodyBytes))
    httpRequest.GetBody = func() (io.ReadCloser, error) {
        return io.NopCloser(bytes.NewReader(bodyBytes)), nil
    }

    return bodyBytes, nil
}

var _ securitycontract.TokenSource = (*HmacTokenSource)(nil)
