package security

import (
    "bytes"
    "io"
    nethttp "net/http"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

const hmacTokenSourceName = "hmacInternal"

/* HmacVerifyBodyBeforeNonceAttribute is the request attribute (settable per route or, on demand, through SetHmacVerifyBodyBeforeNonce) that overrides the source's VerifyBodyBeforeNonce default for a single request. Its value must be a bool. */
const HmacVerifyBodyBeforeNonceAttribute = "security.hmac.verifyBodyBeforeNonce"

/* HmacTokenSourceConfig configures the verifying side of the internal-auth scheme. Secrets resolves the shared key (supporting rotation through multiple resolvable key ids); Apps maps a verified caller to the roles its service principal receives; NonceGuard rejects replayed envelopes (defaults to an in-process guard — supply a shared one for multi-instance deployments); Leeway tolerates clock skew on the issued/expiry checks; MaxFutureExpiry bounds how far ahead an envelope's expiry may sit. */
type HmacTokenSourceConfig struct {
    Secrets    HmacSecretProvider
    Apps       HmacAppRegistry
    NonceGuard securitycontract.NonceGuard
    HeaderName string
    Leeway     time.Duration

    /* MaxFutureExpiry caps how far past the verifier's clock an envelope's ExpiresAt may sit, since the nonce guard remembers each nonce until its envelope expires. Zero leaves the horizon unbounded; a negative value is refused. */
    MaxFutureExpiry time.Duration

    /* ServiceIdentity, when set, turns on audience enforcement: an envelope whose signed Audience differs is refused. Empty skips the audience check. */
    ServiceIdentity string

    /* VerifyBodyBeforeNonce selects the default order of the body and nonce checks. When false (the default) the nonce is consumed before the body is read, so a captured valid envelope can force at most one body buffering — but an on-path party who replays the header with a mutated body burns the nonce and fails the legitimate request as a replay. When true the body hash is verified first, so a body mismatch is rejected without consuming the nonce, at the cost of letting a captured envelope force body buffering until it expires. A per-request override (route attribute HmacVerifyBodyBeforeNonceAttribute, or SetHmacVerifyBodyBeforeNonce) takes precedence for routes/calls that need the opposite trade-off. */
    VerifyBodyBeforeNonce bool

    /* AcceptUntypedEnvelopes admits an envelope that carries no typ, for a rolling upgrade: set it on the verifiers first, roll the signers, then clear it. While it is set, a JSON web token on this header is refused only on its signature; a present, wrong typ is always refused. Default false; the window is removed in v4. */
    AcceptUntypedEnvelopes bool

    /* Clock is the clock the envelope's time window and the nonce ttl are read from; nil uses the system clock. Inject a frozen clock for deterministic tests. */
    Clock clockcontract.Clock
}

func NewHmacTokenSource(config HmacTokenSourceConfig) *HmacTokenSource {
    if true == internal.IsNilInterface(config.Secrets) {
        exception.Panic(exception.NewError("hmac token source secrets provider is nil", nil, nil))
    }

    if true == internal.IsNilInterface(config.Apps) {
        exception.Panic(exception.NewError("hmac token source app registry is nil", nil, nil))
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
        nonceGuard = NewMemoryNonceGuardWithClock(clockInstance)
    }

    /* a negative cap is refused: the check is gated on a positive value, so a negative one would read as unbounded */
    if 0 > config.MaxFutureExpiry {
        exception.Panic(exception.NewError(
            "hmac token source max future expiry may not be negative; a negative value disables the future-expiry cap the field exists to enforce",
            map[string]any{"maxFutureExpiry": config.MaxFutureExpiry.String()},
            nil,
        ))
    }

    return &HmacTokenSource{
        secrets:               config.Secrets,
        apps:                  config.Apps,
        nonceGuard:            nonceGuard,
        headerName:            headerName,
        leeway:                config.Leeway,
        maxFutureExpiry:       config.MaxFutureExpiry,
        verifyBodyBeforeNonce: config.VerifyBodyBeforeNonce,
        acceptUntypedEnvelopes: config.AcceptUntypedEnvelopes,
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
    apps                  HmacAppRegistry
    nonceGuard            securitycontract.NonceGuard
    headerName            string
    leeway                time.Duration
    maxFutureExpiry       time.Duration
    verifyBodyBeforeNonce bool
    serviceIdentity       string
    acceptUntypedEnvelopes bool
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

    envelope, keyId, decodeErr := decodeHmacHeaderValueAcceptingUntypedEnvelopes(headerValue, instance.secrets, instance.acceptUntypedEnvelopes)
    if nil != decodeErr {
        return instance.reject(runtimeInstance, decodeErr)
    }

    /* the key id must be issued to the app the envelope claims, or any holder of a valid secret could impersonate another app */
    boundApp, keyBound := instance.secrets.AppForKeyId(keyId)
    if false == keyBound || boundApp != envelope.App {
        return instance.reject(
            runtimeInstance,
            exception.NewError(
                "internal-auth key id is not authorized for the claimed app",
                map[string]any{"keyId": keyId, "claimedApp": envelope.App, "boundApp": boundApp},
                nil,
            ),
        )
    }

    roles, appKnown := instance.apps.RolesForApp(envelope.App)
    if false == appKnown {
        return instance.reject(
            runtimeInstance,
            exception.NewError("internal-auth app is not known", map[string]any{"app": envelope.App}, nil),
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

    return NewAuthenticatedTokenWithActor(envelope.App, roles, actor), nil
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

    /* the nonce guard remembers the nonce until exp+leeway, so an unbounded expiry would pin memory; a zero maxFutureExpiry leaves it unbounded */
    if 0 < instance.maxFutureExpiry && true == expiry.After(now.Add(instance.maxFutureExpiry)) {
        return exception.NewError("internal-auth envelope expiry is too far in the future", nil, nil)
    }

    deadline := expiry.Add(instance.leeway)
    if true == now.After(deadline) {
        return exception.NewError("internal-auth envelope is expired", nil, nil)
    }

    if 0 < envelope.IssuedAt {
        activation := time.Unix(envelope.IssuedAt, 0).Add(-instance.leeway)
        if true == now.Before(activation) {
            return exception.NewError("internal-auth envelope is not yet valid", nil, nil)
        }
    }

    return nil
}

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
    if 0 >= ttl {
        /* the nonce guard does not record a non-positive ttl, so an envelope at the edge of the window is refused rather than admitted unremembered, and so replayable */
        return exception.NewError("internal-auth envelope is too close to expiry to guard against replay", nil, nil)
    }

    seen, rememberErr := instance.nonceGuard.Remember(runtimeInstance, hmacNonceGuardKey(keyId, envelope.Nonce), ttl)
    if nil != rememberErr {
        /* the guard failing to answer is the platform's failure, marked so reject logs it as an incident */
        return exception.NewError("internal-auth nonce guard failed", nil, markInfrastructureFailure(rememberErr))
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
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil != logger {
        /* every rejection fails closed to anonymous; an infrastructure failure is logged above the Info a bad envelope gets */
        if true == isInfrastructureFailure(cause) {
            logger.Error("internal-auth verification infrastructure failed", exception.LogContext(cause))
        } else {
            logger.Info("internal-auth envelope rejected", exception.LogContext(cause))
        }
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
