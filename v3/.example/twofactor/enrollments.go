package twofactor

import (
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* NewEnrollments answers the enrollments of the store the source resolves for each request, as the sign-in's second factor reads them: the store is resolved when a sign-in needs it, so a store that could not be built refuses that sign-in and the next one asks again. */
func NewEnrollments(source StoreSource) *Enrollments {
    return &Enrollments{source: source}
}

type Enrollments struct {
    source StoreSource
}

func (instance *Enrollments) FindTotpSecret(runtimeInstance melodyruntimecontract.Runtime, userIdentifier string) (string, bool, error) {
    store, storeErr := instance.source(runtimeInstance)
    if nil != storeErr {
        return "", false, storeErr
    }

    return store.FindTotpSecret(runtimeInstance, userIdentifier)
}

func (instance *Enrollments) RedeemRecoveryCode(runtimeInstance melodyruntimecontract.Runtime, userIdentifier string, code string) (bool, error) {
    store, storeErr := instance.source(runtimeInstance)
    if nil != storeErr {
        return false, storeErr
    }

    return store.RedeemRecoveryCode(runtimeInstance, userIdentifier, code)
}

var _ melodysecuritycontract.TwoFactorEnrollmentStore = (*Enrollments)(nil)
var _ melodysecuritycontract.TwoFactorRecoveryStore = (*Enrollments)(nil)
