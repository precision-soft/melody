package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* TwoFactorPending is implemented by the token returned when a primary credential has been accepted but a required second factor (a TOTP code) has not yet been supplied or did not verify. The token is deliberately not authenticated; the application inspects this interface to know it should prompt for a code rather than treating the request as anonymous. */
type TwoFactorPending interface {
    PendingUserIdentifier() string
}

/* TwoFactorEnrollmentStore reports whether a user has a second factor configured and, if so, returns the TOTP secret to verify against. It is supplied by the application because only the application knows where enrollments live (typically an encrypted column). Returning enrolled=false means the user has no second factor and primary authentication stands on its own. */
type TwoFactorEnrollmentStore interface {
    FindTotpSecret(runtimeInstance runtimecontract.Runtime, userIdentifier string) (secret string, enrolled bool, err error)
}

/* TwoFactorRecoveryStore is an optional companion to TwoFactorEnrollmentStore: when the enrollment store also implements it, TotpSecondFactorAuthenticator accepts a single-use recovery code (on its recovery header) as an alternative to a TOTP code. RedeemRecoveryCode must atomically verify the code is one of the user's currently-unused recovery codes and consume it — remove it so a second presentation of the same code cannot succeed — returning redeemed=true only when a previously-unused code was consumed. A store that does not implement this interface simply makes recovery codes unavailable; TOTP verification is unaffected. The atomic check-and-consume (a transaction or a conditional update) is the store's responsibility, mirroring how FindTotpSecret owns enrollment storage. */
type TwoFactorRecoveryStore interface {
    RedeemRecoveryCode(runtimeInstance runtimecontract.Runtime, userIdentifier string, code string) (redeemed bool, err error)
}
