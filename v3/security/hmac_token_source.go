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

    /* MaxFutureExpiry caps how far in the future an envelope's ExpiresAt may sit (measured from the verifier's clock). The nonce guard remembers each nonce until its envelope expires, so without a cap a holder of a valid secret could issue far-future-expiry envelopes and pin unbounded memory in an in-process guard. Zero leaves the horizon unbounded (the previous behaviour) for callers that deliberately mint long-lived envelopes; set it (for example a few minutes above the signer's Ttl) on multi-instance deployments. */
    MaxFutureExpiry time.Duration

    /* ServiceIdentity, when set, is this service's own name and turns on audience enforcement: the verifier rejects any envelope whose signed Audience does not equal it, so a shared caller's envelope captured en route to a different service cannot be replayed here. Opt-in and backward compatible — leaving it empty skips the audience check entirely, so envelopes minted before signers set an Audience keep verifying. Once set, callers must sign with a matching HmacEnvelopeSignerConfig.Audience or their envelopes are rejected. */
    ServiceIdentity string

    /* VerifyBodyBeforeNonce selects the default order of the body and nonce checks. When false (the default) the nonce is consumed before the body is read, so a captured valid envelope can force at most one body buffering — but an on-path party who replays the header with a mutated body burns the nonce and fails the legitimate request as a replay. When true the body hash is verified first, so a body mismatch is rejected without consuming the nonce, at the cost of letting a captured envelope force body buffering until it expires. A per-request override (route attribute HmacVerifyBodyBeforeNonceAttribute, or SetHmacVerifyBodyBeforeNonce) takes precedence for routes/calls that need the opposite trade-off. */
    VerifyBodyBeforeNonce bool

    /* AcceptUntypedEnvelopes opens the migration window for the envelope's own typ, and exists because emitting the typ and requiring it arrived together: a verifier on this version refuses every envelope minted by a signer that has not been redeployed yet, so during a rolling upgrade each unmigrated caller fails before its signature is read and a fleet degrades to anonymous all at once. Set it on the verifiers FIRST, roll the signers, then clear it — that ordering is what makes the upgrade continuous.

       What the window costs while it is open is the structural half of the domain separation: a JSON web token carries no typ, so one presented on this header is no longer refused on its type alone. It is still refused on its signature, which has to verify under the internal-auth secret for the key id it names, and the JWT validator still refuses this envelope's type — so the exposure is a deployment that signs JSON web tokens with the very secret it uses for internal auth. A typ that is present and wrong is refused whether or not this is set.

       Default false, which is the strict reading. The window is removed in v4, where the typ is required unconditionally. */
    AcceptUntypedEnvelopes bool

    /* Clock is the clock the envelope's time window and the nonce ttl are measured against; nil uses the system clock. Inject a frozen clock for deterministic tests. */
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

    /* a negative future-expiry cap is refused rather than carried, the way JwtConfig refuses a negative revocation skew: verifyTimeWindow gates the check on `0 < maxFutureExpiry`, so a negative value — reachable from a config typo like signerTtl - safetyMargin computed below zero — behaves identically to the zero "unbounded" case and silently reopens the memory-pinning window the cap exists to close, a holder of a valid secret minting far-future-expiry envelopes whose nonces the guard remembers until they expire. */
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

    /* the signature only proves the holder of the key id's secret signed the envelope — not that the claimed app owns that key. Without binding the key id to the app, anyone holding any valid secret could claim a higher-privileged app (and forge its actor). Refuse an envelope whose key id is not issued to the app it claims, so a shared or leaked secret cannot be used to impersonate another app. */
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
        /* audience enforcement is opt-in: with no configured ServiceIdentity the envelope's audience is not checked, so envelopes minted before signers set an Audience keep verifying (backward compatible). */
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
        /* a server-originated *http.Request always carries a non-nil URL, but a synthetically constructed request (an internal caller building an *http.Request directly) can leave it nil; guard it so endpoint verification fails closed instead of dereferencing a nil URL and panicking inside the request pipeline */
        return exception.NewError("internal-auth request url is nil", nil, nil)
    }

    if envelope.Method != httpRequest.Method {
        return exception.NewError(
            "internal-auth method does not match the request",
            map[string]any{"signed": envelope.Method, "request": httpRequest.Method},
            nil,
        )
    }

    if envelope.Path != httpRequest.URL.Path {
        return exception.NewError(
            "internal-auth path does not match the request",
            map[string]any{"signed": envelope.Path, "request": httpRequest.URL.Path},
            nil,
        )
    }

    if envelope.Query != httpRequest.URL.RawQuery {
        /* the two query strings carry whatever the caller put in the url — `?token=`, `?api_key=` — and this context lands in the log through reject, so only the parameter NAMES survive into it: they are what makes the mismatch diagnosable, the values are what must not be journaled. */
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

    /* bound how far in the future the expiry may sit. guardNonce remembers the nonce until exp+leeway, so an unbounded expiry lets a holder of a valid secret pin unbounded memory in an in-process nonce guard. A zero maxFutureExpiry keeps the horizon unbounded for callers that deliberately mint long-lived envelopes. */
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
        /* the nonce guard does not record a non-positive ttl, so an envelope at the very edge of the acceptance window would be admitted without ever being remembered — and thus replayable. verifyTimeWindow treats that edge as still valid, so reject it here to keep the recorded window exactly as wide as the accepted one. */
        return exception.NewError("internal-auth envelope is too close to expiry to guard against replay", nil, nil)
    }

    seen, rememberErr := instance.nonceGuard.Remember(runtimeInstance, hmacNonceGuardKey(keyId, envelope.Nonce), ttl)
    if nil != rememberErr {
        /* the guard failing to answer is the platform's failure, not the envelope's: the mark is what makes reject log it as the incident it is — a shared guard down degrades every internal caller to anonymous at once — instead of the routine Info a forged envelope earns. */
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
        /* every rejection fails closed to anonymous, but the two kinds must not share a severity: a bad envelope at Info is routine noise, while an infrastructure failure — the shared nonce guard down — silently degrades every internal caller to anonymous at once, and this record is the only place that incident surfaces. */
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
