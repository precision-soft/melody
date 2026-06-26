package twofactor

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "time"

    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/precision-soft/melody/v3/security/totp"
    bun "github.com/uptrace/bun"
)

/* Enrollment shows how to persist a user's TOTP second factor with the secret and
 * recovery codes encrypted at rest through bunorm's EncryptedString. The recovery
 * codes are stored as an encrypted JSON array. */
type Enrollment struct {
    bun.BaseModel `bun:"table:melody_example_two_factor"`

    UserIdentifier string                        `bun:"user_identifier,pk"`
    Secret         melodyencrypt.EncryptedString `bun:"secret,type:varbinary(512),notnull"`
    RecoveryCodes  melodyencrypt.EncryptedString `bun:"recovery_codes,type:varbinary(2048),notnull"`
    CreatedAt      time.Time                     `bun:"created_at,notnull"`
}

func NewStore(database *bun.DB) *Store {
    return &Store{database: database}
}

type Store struct {
    database *bun.DB
}

/* EnsureSchema creates the demo table. Production code would express this through the
 * bunorm migrate package instead of creating the table inline. */
func (instance *Store) EnsureSchema(ctx context.Context) error {
    _, execErr := instance.database.NewCreateTable().
        Model((*Enrollment)(nil)).
        IfNotExists().
        Exec(ctx)

    return execErr
}

/* Enroll generates a fresh secret and recovery codes for a user, persists them
 * encrypted, and returns the secret, the otpauth URI to render as a QR code, and the
 * plaintext recovery codes to show the user once. */
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

    if _, insertErr := instance.database.NewInsert().Model(enrollment).Exec(ctx); nil != insertErr {
        return "", "", nil, insertErr
    }

    uri := totp.OtpauthURI(issuer, userIdentifier, secret, totp.Config{})

    return secret, uri, recoveryCodes, nil
}

/* FindTotpSecret implements securitycontract.TwoFactorEnrollmentStore, decrypting the
 * stored secret transparently through EncryptedString. */
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
        /* a missing row means the user has no second factor; any other error must
         * fail closed (be returned) rather than be mistaken for "not enrolled", which
         * would silently let primary authentication stand on its own */
        if true == errors.Is(selectErr, sql.ErrNoRows) {
            return "", false, nil
        }

        return "", false, selectErr
    }

    return string(enrollment.Secret), true, nil
}

var _ melodysecuritycontract.TwoFactorEnrollmentStore = (*Store)(nil)
