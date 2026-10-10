package twofactor

import (
    "errors"

    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* ErrStoreUnavailable is the refusal of a sign-in whose second factor store could not be resolved: the login door answers it 503, as the enroll and verify doors answer the same store. */
var ErrStoreUnavailable = errors.New("the second factor store is unavailable")

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
        return "", false, storeUnavailable(storeErr)
    }

    return store.FindTotpSecret(runtimeInstance, userIdentifier)
}

func (instance *Enrollments) RedeemRecoveryCode(runtimeInstance melodyruntimecontract.Runtime, userIdentifier string, code string) (bool, error) {
    store, storeErr := instance.source(runtimeInstance)
    if nil != storeErr {
        return false, storeUnavailable(storeErr)
    }

    return store.RedeemRecoveryCode(runtimeInstance, userIdentifier, code)
}

/* storeUnavailable carries the source's failure under ErrStoreUnavailable, so the door reaches the sentinel through the framework authenticator's wrap and the record keeps the cause */
func storeUnavailable(storeErr error) error {
    return melodyexception.NewError(ErrStoreUnavailable.Error(), nil, errors.Join(ErrStoreUnavailable, storeErr))
}

var _ melodysecuritycontract.TwoFactorEnrollmentStore = (*Enrollments)(nil)
var _ melodysecuritycontract.TwoFactorRecoveryStore = (*Enrollments)(nil)
