package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* TwoFactorPending identifies an unauthenticated token whose primary credential passed but whose required second factor has not verified. */
type TwoFactorPending interface {
    PendingUserIdentifier() string
}

/* TwoFactorEnrollmentStore supplies an enrolled user’s TOTP secret. Returning enrolled=false permits primary authentication alone. */
type TwoFactorEnrollmentStore interface {
    FindTotpSecret(runtimeInstance runtimecontract.Runtime, userIdentifier string) (secret string, enrolled bool, err error)
}

/* TwoFactorRecoveryStore optionally enables single-use recovery codes alongside TOTP. RedeemRecoveryCode must atomically verify and consume an unused code, returning true only for that successful consumption. */
type TwoFactorRecoveryStore interface {
    RedeemRecoveryCode(runtimeInstance runtimecontract.Runtime, userIdentifier string, code string) (redeemed bool, err error)
}
