package twofactor

import (
    "errors"
    "testing"

    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* a store that could not be built refuses the sign-in that needed it, on both doors of the second factor, rather than reading as an account without one */
func TestEnrollmentsAnswerTheStoreRefusalOnBothDoors(t *testing.T) {
    refusal := errors.New("the catalogue database refused the migration")

    enrollments := NewEnrollments(func(runtimeInstance melodyruntimecontract.Runtime) (*Store, error) {
        return nil, refusal
    })

    if _, enrolled, findErr := enrollments.FindTotpSecret(nil, "user-2"); false == errors.Is(findErr, refusal) || true == enrolled {
        t.Fatalf("expected the enrollment read to answer the refusal, got enrolled=%v %v", enrolled, findErr)
    }

    if redeemed, redeemErr := enrollments.RedeemRecoveryCode(nil, "user-2", "recovery-one"); false == errors.Is(redeemErr, refusal) || true == redeemed {
        t.Fatalf("expected the redemption to answer the refusal, got redeemed=%v %v", redeemed, redeemErr)
    }
}
