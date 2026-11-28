package twofactor

import (
    "errors"
    "testing"

    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
)

func TestEnrollmentsAnswerTheStoreRefusalOnBothDoors(t *testing.T) {
    refusal := errors.New("the catalogue database refused the migration")

    enrollments := NewEnrollments(func(runtimeInstance melodycontainercontract.Runtime) (*Store, error) {
        return nil, refusal
    })

    if _, enrolled, findErr := enrollments.FindTotpSecret(nil, "user-2"); false == errors.Is(findErr, refusal) || true == enrolled {
        t.Fatalf("expected the enrollment read to answer the refusal, got enrolled=%v %v", enrolled, findErr)
    }

    if redeemed, redeemErr := enrollments.RedeemRecoveryCode(nil, "user-2", "recovery-one"); false == errors.Is(redeemErr, refusal) || true == redeemed {
        t.Fatalf("expected the redemption to answer the refusal, got redeemed=%v %v", redeemed, redeemErr)
    }
}
