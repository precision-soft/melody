package security

import (
    "time"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/precision-soft/melody/v3/security/totp"
)

/* DefaultTotpCodeHeaderName is the request header the TOTP code is read from unless overridden. */
const DefaultTotpCodeHeaderName = "X-2FA-Code"

/* TotpSecondFactorAuthenticatorConfig composes a primary authenticator with a TOTP second factor into a single Authenticator, so it slots into the existing AuthenticatorManager without changing the manager's first-match flow. When the primary credential is accepted and the user has a TOTP enrollment, a valid code header is additionally required; otherwise the result is a non-authenticated TwoFactorPendingToken the application uses to prompt for a code. */
type TotpSecondFactorAuthenticatorConfig struct {
    Primary securitycontract.Authenticator

    Enrollments securitycontract.TwoFactorEnrollmentStore

    /* CodeHeaderName overrides the header the TOTP code is read from; defaults to DefaultTotpCodeHeaderName. */
    CodeHeaderName string

    Totp totp.Config

    /* ReplayGuard enforces single use of an accepted code within its validity window (the same NonceGuard contract the HMAC source uses). Optional: defaults to an in-process guard, so supply a shared one for multi-instance deployments. */
    ReplayGuard securitycontract.NonceGuard
}

func NewTotpSecondFactorAuthenticator(config TotpSecondFactorAuthenticatorConfig) *TotpSecondFactorAuthenticator {
    if true == internal.IsNilInterface(config.Primary) {
        exception.Panic(exception.NewError("totp second factor primary authenticator is nil", nil, nil))
    }

    if true == internal.IsNilInterface(config.Enrollments) {
        exception.Panic(exception.NewError("totp second factor enrollment store is nil", nil, nil))
    }

    codeHeaderName := config.CodeHeaderName
    if "" == codeHeaderName {
        codeHeaderName = DefaultTotpCodeHeaderName
    }

    /* default to an in-process replay guard (as the HMAC source does) so an accepted code cannot be replayed within its validity window out of the box; multi-instance deployments supply a shared guard. */
    var replayGuard securitycontract.NonceGuard = config.ReplayGuard
    if true == internal.IsNilInterface(replayGuard) {
        replayGuard = NewMemoryNonceGuard()
    }

    return &TotpSecondFactorAuthenticator{
        primary:        config.Primary,
        enrollments:    config.Enrollments,
        codeHeaderName: codeHeaderName,
        totpConfig:     config.Totp,
        replayGuard:    replayGuard,
    }
}

type TotpSecondFactorAuthenticator struct {
    primary        securitycontract.Authenticator
    enrollments    securitycontract.TwoFactorEnrollmentStore
    codeHeaderName string
    totpConfig     totp.Config
    replayGuard    securitycontract.NonceGuard
}

func (instance *TotpSecondFactorAuthenticator) Supports(request httpcontract.Request) bool {
    return instance.primary.Supports(request)
}

func (instance *TotpSecondFactorAuthenticator) Authenticate(request httpcontract.Request) (securitycontract.Token, error) {
    token, authenticateErr := instance.primary.Authenticate(request)
    if nil != authenticateErr {
        return nil, authenticateErr
    }

    if true == internal.IsNilInterface(token) || false == token.IsAuthenticated() {
        return token, nil
    }

    runtimeInstance := request.RuntimeInstance()

    secret, enrolled, findErr := instance.enrollments.FindTotpSecret(runtimeInstance, token.UserIdentifier())
    if nil != findErr {
        return nil, exception.NewError("could not look up two-factor enrollment", nil, findErr)
    }

    if false == enrolled {
        return token, nil
    }

    code := request.Header(instance.codeHeaderName)
    if "" == code {
        return NewTwoFactorPendingToken(token), nil
    }

    verified, verifyErr := totp.Verify(secret, code, instance.totpConfig)
    if nil != verifyErr {
        return nil, exception.NewError("could not verify the two-factor code", nil, verifyErr)
    }

    if false == verified {
        return NewTwoFactorPendingToken(token), nil
    }

    if reused, replayErr := instance.codeAlreadyUsed(request, token.UserIdentifier(), code); nil != replayErr {
        return nil, replayErr
    } else if true == reused {
        return NewTwoFactorPendingToken(token), nil
    }

    return token, nil
}

/* codeAlreadyUsed records an accepted code through the optional replay guard and reports whether it had already been used within its validity window. With no guard configured it always reports unused. */
func (instance *TotpSecondFactorAuthenticator) codeAlreadyUsed(
    request httpcontract.Request,
    userIdentifier string,
    code string,
) (bool, error) {
    if true == internal.IsNilInterface(instance.replayGuard) {
        return false, nil
    }

    nonce := "2fa:" + userIdentifier + ":" + code

    seen, rememberErr := instance.replayGuard.Remember(request.RuntimeInstance(), nonce, instance.codeValidityWindow())
    if nil != rememberErr {
        return false, exception.NewError("two-factor replay guard failed", nil, rememberErr)
    }

    return seen, nil
}

func (instance *TotpSecondFactorAuthenticator) codeValidityWindow() time.Duration {
    resolved := instance.totpConfig
    period := resolved.Period
    if 0 == period {
        period = 30
    }

    skew := resolved.Skew
    if 0 == skew {
        skew = 1
    }

    return time.Duration(period*(2*skew+1)) * time.Second
}

var _ securitycontract.Authenticator = (*TotpSecondFactorAuthenticator)(nil)
