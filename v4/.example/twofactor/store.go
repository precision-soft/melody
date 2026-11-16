package twofactor

import (
    "context"
    "crypto/subtle"
    "database/sql"
    "encoding/json"
    "errors"
    "strings"
    "time"

    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v4/encrypt"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    "github.com/precision-soft/melody/v4/exception"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/security/totp"
    bun "github.com/uptrace/bun"
)

/* SecretColumn binds the enrollment's secret to its column: the seal opens through this column alone, so a seal copied in from another bound column does not read. */
type SecretColumn struct {
    melodyencrypt.DefaultCipherReference
}

func (instance SecretColumn) SealBinding() string {
    return "melody_example_v4_two_factor.secret"
}

/* RecoveryCodesColumn binds the enrollment's recovery codes to their column, as SecretColumn binds the secret. */
type RecoveryCodesColumn struct {
    melodyencrypt.DefaultCipherReference
}

func (instance RecoveryCodesColumn) SealBinding() string {
    return "melody_example_v4_two_factor.recovery_codes"
}

/* PendingSecretColumn binds a pending enrollment's secret to its own column, so a pending seal does not open as an active one. */
type PendingSecretColumn struct {
    melodyencrypt.DefaultCipherReference
}

func (instance PendingSecretColumn) SealBinding() string {
    return "melody_example_v4_two_factor_pending.secret"
}

/* PendingRecoveryCodesColumn binds a pending enrollment's recovery codes to their own column. */
type PendingRecoveryCodesColumn struct {
    melodyencrypt.DefaultCipherReference
}

func (instance PendingRecoveryCodesColumn) SealBinding() string {
    return "melody_example_v4_two_factor_pending.recovery_codes"
}

/* SealBindings declares the four bound columns to the encrypt module. Both refuse an unbound value, which holds for a database whose every enrollment was written through the bound types; an enrollment written before the columns were bound is refused and is cleared with example:twofactor:reset or example:db:reset --force. */
func SealBindings() []melodyencrypt.SealBindingConfig {
    return []melodyencrypt.SealBindingConfig{
        {Reference: SecretColumn{}, RefuseUnbound: true},
        {Reference: RecoveryCodesColumn{}, RefuseUnbound: true},
        {Reference: PendingSecretColumn{}, RefuseUnbound: true},
        {Reference: PendingRecoveryCodesColumn{}, RefuseUnbound: true},
    }
}

/* Enrollment is a user's TOTP second factor, its secret and its recovery codes, an encrypted JSON array, encrypted at rest through bunorm's EncryptedStringFor, each sealed bound to its column. */
type Enrollment struct {
    bun.BaseModel `bun:"table:melody_example_v4_two_factor"`

    UserIdentifier string                                                `bun:"user_identifier,pk"`
    Secret         melodyencrypt.EncryptedStringFor[SecretColumn]        `bun:"secret,type:varbinary(512),notnull"`
    RecoveryCodes  melodyencrypt.EncryptedStringFor[RecoveryCodesColumn] `bun:"recovery_codes,type:varbinary(2048),notnull"`
    CreatedAt      time.Time                                             `bun:"created_at,notnull"`
}

/* PendingEnrollment is a secret and its recovery codes minted and not yet confirmed: no factor until a code its owner reads off the authenticator moves it into the active table. */
type PendingEnrollment struct {
    bun.BaseModel `bun:"table:melody_example_v4_two_factor_pending"`

    UserIdentifier string                                                       `bun:"user_identifier,pk"`
    Secret         melodyencrypt.EncryptedStringFor[PendingSecretColumn]        `bun:"secret,type:varbinary(512),notnull"`
    RecoveryCodes  melodyencrypt.EncryptedStringFor[PendingRecoveryCodesColumn] `bun:"recovery_codes,type:varbinary(2048),notnull"`
    ReplacesFactor bool                                                         `bun:"replaces_factor,notnull"`
    CreatedAt      time.Time                                                    `bun:"created_at,notnull"`
}

/* PendingEnrollmentLifetime is how long a pending enrollment stays confirmable: a set-up abandoned, its key photographed or copied, does not stay a factor in waiting for the life of the account */
const PendingEnrollmentLifetime = 15 * time.Minute

func NewStore(database *bun.DB) *Store {
    return &Store{database: database}
}

/* ServiceStore is the container name of the store the two doors and the enrollment release read. */
const ServiceStore = "service-example-two-factor-store"

/* StoreSource answers the store a door acts on, at the moment it acts: StoreFromRuntime in production, a fixed store in tests. */
type StoreSource func(runtimeInstance melodyruntimecontract.Runtime) (*Store, error)

/* StoreFromRuntime resolves the store through the container, the single door that does so, when a request or a deletion needs it. Building the store applies the migration set to the catalogue's database, and the container keeps only a successful resolution, so a refusal is answered to the caller who met it and the next one asks again. */
func StoreFromRuntime(runtimeInstance melodyruntimecontract.Runtime) (*Store, error) {
    store, storeErr := melodycontainer.FromResolver[*Store](runtimeInstance.Context(), runtimeInstance.Container(), ServiceStore)
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

/* Enroll generates a fresh secret and recovery codes for a user, persists them encrypted as a PENDING enrollment, and answers the secret, the otpauth URI to render as a QR code and the plaintext recovery codes to show once. A pending enrollment is no factor: the active one, if any, keeps verifying until ConfirmEnrollment moves this one in. Enrolling again while one is pending replaces the pending one, the case of a response its owner never received. replacesFactor records that the caller presented the active factor, the only grant under which the confirmation may replace one. */
func (instance *Store) Enroll(
    ctx context.Context,
    userIdentifier string,
    issuer string,
    replacesFactor bool,
) (string, string, []string, error) {
    secret, secretErr := totp.GenerateSecret()
    if nil != secretErr {
        return "", "", nil, secretErr
    }

    uri, uriErr := totp.OtpauthUri(issuer, userIdentifier, secret, totp.Config{})
    if nil != uriErr {
        return "", "", nil, uriErr
    }

    recoveryCodes, recoveryErr := totp.GenerateRecoveryCodes(0)
    if nil != recoveryErr {
        return "", "", nil, recoveryErr
    }

    encodedCodes, marshalErr := json.Marshal(recoveryCodes)
    if nil != marshalErr {
        return "", "", nil, marshalErr
    }

    pending := &PendingEnrollment{
        UserIdentifier: userIdentifier,
        Secret:         melodyencrypt.EncryptedStringFor[PendingSecretColumn](secret),
        RecoveryCodes:  melodyencrypt.EncryptedStringFor[PendingRecoveryCodesColumn](encodedCodes),
        ReplacesFactor: replacesFactor,
        CreatedAt:      time.Now(),
    }

    if _, insertErr := enrollmentUpsert(instance.database, pending).Set("replaces_factor = VALUES(replaces_factor)").Exec(ctx); nil != insertErr {
        return "", "", nil, insertErr
    }

    return secret, uri, recoveryCodes, nil
}

/* FindPendingSecret answers the secret of the user's pending enrollment still within its lifetime, for the confirmation to verify a code against */
func (instance *Store) FindPendingSecret(ctx context.Context, userIdentifier string) (string, bool, error) {
    pending := &PendingEnrollment{}

    selectErr := instance.database.NewSelect().Model(pending).Where("user_identifier = ?", userIdentifier).Where("created_at > ?", pendingCutoff()).Limit(1).Scan(ctx)
    if nil != selectErr {
        if true == errors.Is(selectErr, sql.ErrNoRows) {
            return "", false, nil
        }

        return "", false, selectErr
    }

    return string(pending.Secret), true, nil
}

/* ConfirmOutcome is what a confirmation did */
type ConfirmOutcome int

const (
    /* ConfirmMoved: the pending enrollment is the account's factor */
    ConfirmMoved ConfirmOutcome = iota
    /* ConfirmNothingPending: no pending enrollment within its lifetime */
    ConfirmNothingPending
    /* ConfirmReplaced: the pending secret is not the one the code was verified against, a second enroll in between */
    ConfirmReplaced
    /* ConfirmFactorBecameActive: a factor became active after a set-up started without one, and replacing it asks for it */
    ConfirmFactorBecameActive
)

/* ConfirmEnrollment moves the user's pending enrollment into the active table in one transaction holding the pending row and the active one; the caller verified a code against the pending secret first. A pending enrollment started without presenting a factor replaces none: when a factor became active since, the move is refused, so a session alone never displaces a factor it did not present. */
func (instance *Store) ConfirmEnrollment(ctx context.Context, userIdentifier string, verifiedSecret string) (ConfirmOutcome, error) {
    outcome := ConfirmNothingPending

    txErr := instance.database.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
        pending := &PendingEnrollment{}

        selectErr := tx.NewSelect().Model(pending).Where("user_identifier = ?", userIdentifier).Where("created_at > ?", pendingCutoff()).For("UPDATE").Limit(1).Scan(ctx)
        if nil != selectErr {
            if true == errors.Is(selectErr, sql.ErrNoRows) {
                return nil
            }

            return selectErr
        }

        if 1 != subtle.ConstantTimeCompare([]byte(pending.Secret), []byte(verifiedSecret)) {
            outcome = ConfirmReplaced

            return nil
        }

        if false == pending.ReplacesFactor {
            var activeIdentifier string
            activeErr := tx.NewSelect().Table("melody_example_v4_two_factor").Column("user_identifier").Where("user_identifier = ?", userIdentifier).For("UPDATE").Limit(1).Scan(ctx, &activeIdentifier)
            if nil == activeErr {
                outcome = ConfirmFactorBecameActive

                return nil
            }

            if false == errors.Is(activeErr, sql.ErrNoRows) {
                return activeErr
            }
        }

        enrollment := &Enrollment{
            UserIdentifier: userIdentifier,
            Secret:         melodyencrypt.EncryptedStringFor[SecretColumn](string(pending.Secret)),
            RecoveryCodes:  melodyencrypt.EncryptedStringFor[RecoveryCodesColumn](string(pending.RecoveryCodes)),
            CreatedAt:      time.Now(),
        }

        if _, upsertErr := enrollmentUpsert(tx, enrollment).Exec(ctx); nil != upsertErr {
            return upsertErr
        }

        if _, deleteErr := tx.NewDelete().Model((*PendingEnrollment)(nil)).Where("user_identifier = ?", userIdentifier).Exec(ctx); nil != deleteErr {
            return deleteErr
        }

        outcome = ConfirmMoved

        return nil
    })
    if nil != txErr {
        return ConfirmNothingPending, txErr
    }

    return outcome, nil
}

/* EnrollmentState answers whether the user has an active factor and a pending enrollment within its lifetime, reading no sealed column */
func (instance *Store) EnrollmentState(ctx context.Context, userIdentifier string) (bool, bool, error) {
    enrolled, enrolledErr := instance.database.NewSelect().Table("melody_example_v4_two_factor").Where("user_identifier = ?", userIdentifier).Exists(ctx)
    if nil != enrolledErr {
        return false, false, enrolledErr
    }

    pending, pendingErr := instance.database.NewSelect().Table("melody_example_v4_two_factor_pending").Where("user_identifier = ?", userIdentifier).Where("created_at > ?", pendingCutoff()).Exists(ctx)
    if nil != pendingErr {
        return false, false, pendingErr
    }

    return enrolled, pending, nil
}

func pendingCutoff() time.Time {
    return time.Now().UTC().Add(-PendingEnrollmentLifetime)
}

/* enrollmentUpsert is the write of an active or a pending enrollment kept as a query, so the clause that makes a second one a replacement is readable on its own. All three columns are written: the recovery codes belong to the secret they were minted beside. */
func enrollmentUpsert(database bun.IDB, model any) *bun.InsertQuery {
    return database.
        NewInsert().
        Model(model).
        On("DUPLICATE KEY UPDATE").
        Set("secret = VALUES(secret)").
        Set("recovery_codes = VALUES(recovery_codes)").
        Set("created_at = VALUES(created_at)")
}

/* DeleteEnrollment removes an account's second factor, secret and recovery codes together, and an enrollment it has pending, in one transaction; it answers whether either was there. A deleted account's identifier is not minted again, but example:db:reset hands identifiers out anew, and an account must not start enrolled. Deleting nothing is not a failure. */
func (instance *Store) DeleteEnrollment(
    runtimeInstance melodyruntimecontract.Runtime,
    userIdentifier string,
) (bool, error) {
    removed := false

    txErr := instance.database.RunInTx(runtimeInstance.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
        pendingResult, pendingErr := tx.NewDelete().Model((*PendingEnrollment)(nil)).Where("user_identifier = ?", userIdentifier).Exec(ctx)
        if nil != pendingErr {
            return pendingErr
        }

        activeResult, deleteErr := enrollmentDelete(tx, userIdentifier).Exec(ctx)
        if nil != deleteErr {
            return deleteErr
        }

        for _, result := range []sql.Result{pendingResult, activeResult} {
            affected, affectedErr := result.RowsAffected()
            if nil != affectedErr {
                return affectedErr
            }

            removed = removed || 0 < affected
        }

        return nil
    })
    if nil != txErr {
        return false, txErr
    }

    return removed, nil
}

func enrollmentDelete(database bun.IDB, userIdentifier string) *bun.DeleteQuery {
    return database.
        NewDelete().
        Model((*Enrollment)(nil)).
        Where("user_identifier = ?", userIdentifier)
}

/* FindTotpSecret implements securitycontract.TwoFactorEnrollmentStore, decrypting the stored secret through its bound column type. */
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

/* RedeemRecoveryCode implements securitycontract.TwoFactorRecoveryStore. Inside a transaction it loads the enrollment FOR UPDATE and, on a constant-time match, removes the code and writes the re-encrypted remainder back, so a code is never redeemed twice even under concurrent requests. The code is compared in its canonical form: case, spaces and the hyphen do not count. It reports false with a nil error when there is no enrollment or the code is not an unused one. */
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
                if true == recoveryCodeMatches(candidate, code) {
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

            enrollment.RecoveryCodes = melodyencrypt.EncryptedStringFor[RecoveryCodesColumn](encodedCodes)

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

/* recoveryCodeMatches compares a stored recovery code with the one presented in their canonical form, in constant time, so timing does not reveal how much of a code matched; the stored side is canonicalised too, so a hand-edited row compares as honestly as a minted one */
func recoveryCodeMatches(candidate string, presented string) bool {
    return 1 == subtle.ConstantTimeCompare([]byte(canonicalRecoveryCode(candidate)), []byte(canonicalRecoveryCode(presented)))
}

/* canonicalRecoveryCode is a recovery code as it is minted: lower case, without the hyphen or a space a reader copying it may add or drop */
func canonicalRecoveryCode(code string) string {
    return strings.NewReplacer(" ", "", "-", "").Replace(strings.ToLower(code))
}
