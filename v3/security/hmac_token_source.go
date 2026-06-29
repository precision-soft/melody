package security

import (
    "bytes"
    "io"
    nethttp "net/http"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

const hmacTokenSourceName = "hmacInternal"

/* HmacTokenSourceConfig configures the verifying side of the internal-auth scheme. Secrets resolves the shared key (supporting rotation through multiple resolvable key ids); Apps maps a verified caller to the roles its service principal receives; NonceGuard rejects replayed envelopes (defaults to an in-process guard — supply a shared one for multi-instance deployments); Leeway tolerates clock skew on the issued/expiry checks. */
type HmacTokenSourceConfig struct {
    Secrets    HmacSecretProvider
    Apps       HmacAppRegistry
    NonceGuard securitycontract.NonceGuard
    HeaderName string
    Leeway     time.Duration
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

    var nonceGuard securitycontract.NonceGuard = config.NonceGuard
    if true == internal.IsNilInterface(nonceGuard) {
        nonceGuard = NewMemoryNonceGuard()
    }

    return &HmacTokenSource{
        secrets:    config.Secrets,
        apps:       config.Apps,
        nonceGuard: nonceGuard,
        headerName: headerName,
        leeway:     config.Leeway,
    }
}

type HmacTokenSource struct {
    secrets    HmacSecretProvider
    apps       HmacAppRegistry
    nonceGuard securitycontract.NonceGuard
    headerName string
    leeway     time.Duration
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

    envelope, keyId, decodeErr := decodeHmacHeaderValue(headerValue, instance.secrets)
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

    if bindErr := instance.verifyEndpoint(envelope, request); nil != bindErr {
        return instance.reject(runtimeInstance, bindErr)
    }

    if timeErr := instance.verifyTimeWindow(envelope, time.Now()); nil != timeErr {
        return instance.reject(runtimeInstance, timeErr)
    }

    /* the nonce is consumed before the body is read so a captured-but-valid envelope can force at most one body buffering: a replay is rejected here, before readAndRestoreBody runs. The signature has already been verified above, so only a legitimate envelope ever reaches this point. */
    if replayErr := instance.guardNonce(runtimeInstance, envelope); nil != replayErr {
        return instance.reject(runtimeInstance, replayErr)
    }

    if bodyErr := instance.verifyBody(envelope, request); nil != bodyErr {
        return instance.reject(runtimeInstance, bodyErr)
    }

    var actor securitycontract.Actor
    if rebuilt := NewActorFromData(envelope.Actor); nil != rebuilt {
        actor = rebuilt
    }

    return NewAuthenticatedTokenWithActor(envelope.App, roles, actor), nil
}

func (instance *HmacTokenSource) verifyEndpoint(envelope hmacEnvelope, request httpcontract.Request) error {
    httpRequest := request.HttpRequest()
    if nil == httpRequest {
        return exception.NewError("internal-auth request is nil", nil, nil)
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
        return exception.NewError(
            "internal-auth query does not match the request",
            map[string]any{"signed": envelope.Query, "request": httpRequest.URL.RawQuery},
            nil,
        )
    }

    return nil
}

func (instance *HmacTokenSource) verifyTimeWindow(envelope hmacEnvelope, now time.Time) error {
    if 0 >= envelope.ExpiresAt {
        return exception.NewError("internal-auth envelope is missing an expiry", nil, nil)
    }

    deadline := time.Unix(envelope.ExpiresAt, 0).Add(instance.leeway)
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

func (instance *HmacTokenSource) guardNonce(runtimeInstance runtimecontract.Runtime, envelope hmacEnvelope) error {
    if "" == envelope.Nonce {
        return exception.NewError("internal-auth envelope is missing a nonce", nil, nil)
    }

    ttl := time.Until(time.Unix(envelope.ExpiresAt, 0).Add(instance.leeway))
    if 0 >= ttl {
        /* the nonce guard does not record a non-positive ttl, so an envelope at the very edge of the acceptance window would be admitted without ever being remembered — and thus replayable. verifyTimeWindow treats that edge as still valid, so reject it here to keep the recorded window exactly as wide as the accepted one. */
        return exception.NewError("internal-auth envelope is too close to expiry to guard against replay", nil, nil)
    }

    seen, rememberErr := instance.nonceGuard.Remember(runtimeInstance, envelope.Nonce, ttl)
    if nil != rememberErr {
        return exception.NewError("internal-auth nonce guard failed", nil, rememberErr)
    }

    if true == seen {
        return exception.NewError("internal-auth nonce has already been used", nil, nil)
    }

    return nil
}

func (instance *HmacTokenSource) reject(
    runtimeInstance runtimecontract.Runtime,
    cause error,
) (securitycontract.Token, error) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil != logger {
        logger.Info("internal-auth envelope rejected", exception.LogContext(cause))
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
