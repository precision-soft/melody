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
