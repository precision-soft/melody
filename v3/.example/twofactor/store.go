package twofactor

import (
    "context"
    "crypto/subtle"
    "database/sql"
    "encoding/json"
    "errors"
    "time"

    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/precision-soft/melody/v3/security/totp"
    bun "github.com/uptrace/bun"
)

/* Enrollment is a user's TOTP second factor, its secret and its recovery codes, an encrypted JSON array, encrypted at rest through bunorm's EncryptedString. */
type Enrollment struct {
    bun.BaseModel `bun:"table:melody_example_v3_two_factor"`

    UserIdentifier string                        `bun:"user_identifier,pk"`
    Secret         melodyencrypt.EncryptedString `bun:"secret,type:varbinary(512),notnull"`
    RecoveryCodes  melodyencrypt.EncryptedString `bun:"recovery_codes,type:varbinary(2048),notnull"`
    CreatedAt      time.Time                     `bun:"created_at,notnull"`
}

func NewStore(database *bun.DB) *Store {
    return &Store{database: database}
}

/* ServiceStore is the container name of the store the two doors and the enrollment release read. */
const ServiceStore = "service-example-two-factor-store"

/* StoreSource answers the store a door acts on, at the moment it acts: StoreFromRuntime in production, a fixed store in tests. */
type StoreSource func(runtimeInstance melodyruntimecontract.Runtime) (*Store, error)

/* StoreFromRuntime resolves the store through the container, the single door that does so, when a request or a deletion needs it. Building the store applies the migration set to the catalogue's database, and the container keeps only a successful resolution, so a refusal is answered to the caller who met it and the next one asks again. */
func StoreFromRuntime(runtimeInstance melodyruntimecontract.Runtime) (*Store, error) {
    store, storeErr := melodycontainer.FromResolver[*Store](runtimeInstance.Container(), ServiceStore)
    if nil != storeErr {
        return nil, storeErr
    }

    if nil == store {
        return nil, exception.NewError("the two-factor store resolved to nothing", nil, nil)
    }

    return store, nil
}

type Store struct {
    database *bun.DB
}

/* Enroll generates a fresh secret and recovery codes for a user, persists them encrypted, and answers the secret, the otpauth URI to render as a QR code and the plaintext recovery codes to show once. It replaces an existing enrollment, since a lost authenticator is the ordinary reason to enroll again; that is safe because the handler takes the identifier from the authenticated token, so the row replaced is the caller's own, and the previous secret and unused codes stop verifying at once. */
func (instance *Store) Enroll(
    ctx context.Context,
    userIdentifier string,
    issuer string,
) (string, string, []string, error) {
    secret, secretErr := totp.GenerateSecret()
    if nil != secretErr {
        return "", "", nil, secretErr
    }

    recoveryCodes, recoveryErr := totp.GenerateRecoveryCodes(0)
    if nil != recoveryErr {
        return "", "", nil, recoveryErr
    }

    encodedCodes, marshalErr := json.Marshal(recoveryCodes)
    if nil != marshalErr {
        return "", "", nil, marshalErr
    }

    enrollment := &Enrollment{
        UserIdentifier: userIdentifier,
        Secret:         melodyencrypt.EncryptedString(secret),
        RecoveryCodes:  melodyencrypt.EncryptedString(encodedCodes),
        CreatedAt:      time.Now(),
    }

    if _, insertErr := instance.enrollmentUpsert(enrollment).Exec(ctx); nil != insertErr {
        return "", "", nil, insertErr
    }

    uri := totp.OtpauthUri(issuer, userIdentifier, secret, totp.Config{})

    return secret, uri, recoveryCodes, nil
}

/* enrollmentUpsert is the write kept as a query, so the clause that makes a second enrollment a replacement is readable on its own. All three columns are written: the recovery codes belong to the secret they were minted beside. */
func (instance *Store) enrollmentUpsert(enrollment *Enrollment) *bun.InsertQuery {
    return instance.database.
        NewInsert().
        Model(enrollment).
        On("DUPLICATE KEY UPDATE").
        Set("secret = VALUES(secret)").
        Set("recovery_codes = VALUES(recovery_codes)").
        Set("created_at = VALUES(created_at)")
}

/* DeleteEnrollment removes an account's second factor, secret and recovery codes together. Identifiers are minted as the highest suffix plus one, so a deleted account's identifier becomes the next account's, which must not start enrolled. Deleting nothing is not a failure. */
func (instance *Store) DeleteEnrollment(
    runtimeInstance melodyruntimecontract.Runtime,
    userIdentifier string,
) (bool, error) {
    result, deleteErr := instance.enrollmentDelete(userIdentifier).Exec(runtimeInstance.Context())
    if nil != deleteErr {
        return false, deleteErr
    }

    affected, affectedErr := result.RowsAffected()
    if nil != affectedErr {
        return false, affectedErr
    }

    return 0 < affected, nil
}

func (instance *Store) enrollmentDelete(userIdentifier string) *bun.DeleteQuery {
    return instance.database.
        NewDelete().
        Model((*Enrollment)(nil)).
        Where("user_identifier = ?", userIdentifier)
}

/* FindTotpSecret implements securitycontract.TwoFactorEnrollmentStore, decrypting the stored secret through EncryptedString. */
func (instance *Store) FindTotpSecret(
    runtimeInstance melodyruntimecontract.Runtime,
    userIdentifier string,
) (string, bool, error) {
    enrollment := &Enrollment{}

    selectErr := instance.database.NewSelect().
        Model(enrollment).
        Where("user_identifier = ?", userIdentifier).
        Limit(1).
        Scan(runtimeInstance.Context())
    if nil != selectErr {
        /* a missing row means no second factor; any other error fails closed rather than reading as "not enrolled", which would let primary authentication stand alone */
        if true == errors.Is(selectErr, sql.ErrNoRows) {
            return "", false, nil
        }

        return "", false, selectErr
    }

    return string(enrollment.Secret), true, nil
}

/* RedeemRecoveryCode implements securitycontract.TwoFactorRecoveryStore. Inside a transaction it loads the enrollment FOR UPDATE and, on a constant-time match, removes the code and writes the re-encrypted remainder back, so a code is never redeemed twice even under concurrent requests. It reports false with a nil error when there is no enrollment or the code is not an unused one. */
func (instance *Store) RedeemRecoveryCode(
    runtimeInstance melodyruntimecontract.Runtime,
    userIdentifier string,
    code string,
) (bool, error) {
    if "" == code {
        return false, nil
    }

    redeemed := false

    txErr := instance.database.RunInTx(
        runtimeInstance.Context(),
        nil,
        func(ctx context.Context, tx bun.Tx) error {
            enrollment := &Enrollment{}

            selectErr := tx.NewSelect().
                Model(enrollment).
                Where("user_identifier = ?", userIdentifier).
                For("UPDATE").
                Limit(1).
                Scan(ctx)
            if nil != selectErr {
                /* a missing row means no second factor: nothing to redeem, as in FindTotpSecret */
                if true == errors.Is(selectErr, sql.ErrNoRows) {
                    return nil
                }

                return selectErr
            }

            var codes []string
            if unmarshalErr := json.Unmarshal([]byte(enrollment.RecoveryCodes), &codes); nil != unmarshalErr {
                return unmarshalErr
            }

            remaining := make([]string, 0, len(codes))
            for _, candidate := range codes {
                /* constant-time compare, so timing does not reveal how much of a recovery code matched */
                if 1 == subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) {
                    redeemed = true

                    continue
                }

                remaining = append(remaining, candidate)
            }

            if false == redeemed {
                return nil
            }

            encodedCodes, marshalErr := json.Marshal(remaining)
            if nil != marshalErr {
                return marshalErr
            }

            enrollment.RecoveryCodes = melodyencrypt.EncryptedString(encodedCodes)

            _, updateErr := tx.NewUpdate().
                Model(enrollment).
                Column("recovery_codes").
                WherePK().
                Exec(ctx)

            return updateErr
        },
    )
    if nil != txErr {
        return false, txErr
    }

    return redeemed, nil
}

var _ melodysecuritycontract.TwoFactorEnrollmentStore = (*Store)(nil)
var _ melodysecuritycontract.TwoFactorRecoveryStore = (*Store)(nil)
