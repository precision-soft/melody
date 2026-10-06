package repository

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "strings"

    melodyaudit "github.com/precision-soft/melody/integrations/bunorm/v3/audit"
    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/uptrace/bun"
)

/* userRow is the directory as the database holds it. Roles are one comma-separated column: a short fixed vocabulary with no commas in it. */
type userRow struct {
    bun.BaseModel `bun:"table:melody_example_v3_user,alias:example_user"`

    Id       string `bun:"id,pk"`
    Username string `bun:"username,notnull"`
    /* the name as NormalizedUsername folds it, derived from Username on every write and never read back into the entity */
    UsernameNormalized string `bun:"username_normalized,notnull"`
    /* the trail records that the password changed and never its values: a history of credentials is what an audit trail must not become */
    Password string `bun:"password,notnull" audit:"redact"`
    Roles    string `bun:"roles,notnull"`
}

func newUserRow(user *entity.User) *userRow {
    return &userRow{
        Id:                 user.Id,
        Username:           user.Username,
        UsernameNormalized: NormalizedUsername(user.Username),
        Password:           user.Password,
        Roles:              strings.Join(user.Roles, ","),
    }
}

func (instance *userRow) toEntity() *entity.User {
    roles := make([]string, 0)

    for _, role := range strings.Split(instance.Roles, ",") {
        trimmedRole := strings.TrimSpace(role)
        if "" == trimmedRole {
            continue
        }

        roles = append(roles, trimmedRole)
    }

    return entity.NewUser(instance.Id, instance.Username, instance.Password, roles)
}

func newBunUserRepository(storage *persistence.CatalogStorage) *bunUserRepository {
    return &bunUserRepository{database: storage.Database(), tracker: storage.Tracker(), recorder: storage.Recorder(), seedsAccounts: storage.SeedsAccounts()}
}

/* bunUserRepository keeps the directory in the database and its history beside it. Every write goes through the audit tracker, the password recorded as changed without its value; GrantRole, which runs its own transaction, records through the recorder inside it. */
/* userIdentifierMintLockName names the advisory lock the creates of melody_example_v3_user mint their identifiers under */
const userIdentifierMintLockName = "melody_example_v3_user.id"

type bunUserRepository struct {
    database      *bun.DB
    tracker       *melodyaudit.Tracker
    recorder      *melodyaudit.Recorder
    seedsAccounts bool
}

/* seedIfEmpty installs the example's accounts on an empty table, through the trail, when the storage was marked to (see persistence.CatalogStorage.WithAccountSeed); otherwise the directory starts empty and stays so until example:user:create */
func (instance *bunUserRepository) seedIfEmpty(ctx context.Context) error {
    if false == instance.seedsAccounts {
        return nil
    }

    identifierList := make([]string, 0)
    for _, user := range seedUserList() {
        identifierList = append(identifierList, user.Id)
    }

    if raiseErr := raiseSequenceOverSeeds(ctx, instance.database, "user-", identifierList); nil != raiseErr {
        return raiseErr
    }

    return seedIfEmptyAudited(ctx, instance.database, instance.recorder, persistence.AuditEntityUser, func() []*userRow {
        seedList := seedUserList()
        rowList := make([]*userRow, 0, len(seedList))
        for _, user := range seedList {
            rowList = append(rowList, newUserRow(user))
        }

        return rowList
    }, func(row *userRow) string {
        return row.Id
    })
}

func (instance *bunUserRepository) All(ctx context.Context) ([]*entity.User, error) {
    rowList := make([]*userRow, 0)

    selectErr := instance.database.
        NewSelect().
        Model(&rowList).
        Order("id ASC").
        Scan(ctx)
    if nil != selectErr {
        return nil, selectErr
    }

    users := make([]*entity.User, 0, len(rowList))
    for _, row := range rowList {
        users = append(users, row.toEntity())
    }

    return users, nil
}

func (instance *bunUserRepository) FindById(ctx context.Context, id string) (*entity.User, bool, error) {
    row, found, findErr := instance.findRowById(ctx, id)
    if nil != findErr {
        return nil, false, findErr
    }

    if false == found {
        return nil, false, nil
    }

    return row.toEntity(), true, nil
}

func (instance *bunUserRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    wanted := NormalizedUsername(username)
    if "" == wanted {
        return nil, false, nil
    }

    row := &userRow{}

    selectErr := instance.userByUsernameQuery(row, wanted).Scan(ctx)
    if nil != selectErr {
        if true == errors.Is(selectErr, sql.ErrNoRows) {
            return nil, false, nil
        }

        return nil, false, selectErr
    }

    return row.toEntity(), true, nil
}

/* findRowById separates a row that is not there from a query that could not run: only sql.ErrNoRows is an answer. */
func (instance *bunUserRepository) findRowById(ctx context.Context, id string) (*userRow, bool, error) {
    row := &userRow{}

    selectErr := instance.database.
        NewSelect().
        Model(row).
        Where("id = ?", id).
        Limit(1).
        Scan(ctx)
    if nil != selectErr {
        if true == errors.Is(selectErr, sql.ErrNoRows) {
            return nil, false, nil
        }

        return nil, false, selectErr
    }

    return row, true, nil
}

func (instance *bunUserRepository) Create(ctx context.Context, user *entity.User) error {
    validationErr := validateUser(user)
    if nil != validationErr {
        return validationErr
    }

    _, usernameExists, usernameErr := instance.FindByUsername(ctx, user.Username)
    if nil != usernameErr {
        return usernameErr
    }

    if true == usernameExists {
        return ErrUsernameAlreadyExists
    }

    mintsIdentifier := "" == strings.TrimSpace(user.Id)
    if false == mintsIdentifier {
        if ceilingErr := refuseIdentifierAtCeiling(user.Id, "user-"); nil != ceilingErr {
            return ceilingErr
        }

        /* a supplied id that is occupied is answered "id already exists" before the insert, as in the sibling repositories, rather than as the primary key's raw duplicate-key text */
        _, occupied, occupiedErr := instance.findRowById(ctx, user.Id)
        if nil != occupiedErr {
            return occupiedErr
        }

        if true == occupied {
            return ErrIdAlreadyExists
        }
    }

    insertErr := insertWithMintedIdentifier(
        ctx,
        instance.database,
        userIdentifierMintLockName,
        identifierSequence{prefix: "user-", identifier: func() string { return user.Id }},
        mintsIdentifier,
        func(floor string) error {
            identifierList, identifierErr := instance.identifierList(ctx)
            if nil != identifierErr {
                return identifierErr
            }

            user.Id = nextUserId(append(identifierList, floor))

            return nil
        },
        func() error {
            return instance.tracker.Insert(auditContext(ctx), persistence.AuditEntityUser, user.Id, newUserRow(user))
        },
    )

    return asUsernameAlreadyExists(insertErr)
}

/* Update reads the row locked FOR UPDATE, asks the guard, and writes the change over what it read in the same transaction, the GrantRole door's shape: an admin update and a grant of the same account serialise on the row, and a field the change leaves out is the one the row holds, never a copy the caller read earlier. The audit entry is recorded through the same transaction. */
func (instance *bunUserRepository) Update(ctx context.Context, id string, change UserChange, guard UserGuard) (*entity.User, *entity.User, error) {
    trimmedId := strings.TrimSpace(id)
    if "" == trimmedId {
        return nil, nil, fmt.Errorf("id is required")
    }

    var beforeAccount *entity.User
    var afterAccount *entity.User
    var refusal error

    txErr := instance.database.RunInTx(auditContext(ctx), nil, func(ctx context.Context, tx bun.Tx) error {
        before, found, lockErr := lockUserRow(ctx, tx, trimmedId)
        if nil != lockErr || false == found {
            return lockErr
        }

        current := before.toEntity()
        if refusal = admittedBy(guard, current); nil != refusal {
            return nil
        }

        changed := change.applyTo(current)
        if refusal = validateUser(changed); nil != refusal {
            return nil
        }

        takenByAnother, takenErr := usernameTakenByAnother(ctx, tx, changed.Username, trimmedId)
        if nil != takenErr {
            return takenErr
        }

        if true == takenByAnother {
            refusal = ErrUsernameAlreadyExists

            return nil
        }

        after := newUserRow(changed)
        if _, updateErr := tx.NewUpdate().Model(after).WherePK().Exec(ctx); nil != updateErr {
            return updateErr
        }

        recordErr := instance.recorder.RecordUpdate(melodyaudit.WithDatabase(ctx, tx), persistence.AuditEntityUser, trimmedId, before, after)
        if nil != recordErr {
            return recordErr
        }

        beforeAccount = current
        afterAccount = after.toEntity()

        return nil
    })
    if nil != refusal {
        return nil, nil, refusal
    }

    if nil != txErr {
        if usernameErr := asUsernameAlreadyExists(txErr); true == errors.Is(usernameErr, ErrUsernameAlreadyExists) {
            return nil, nil, usernameErr
        }

        return nil, nil, exception.NewError(
            "updating the "+persistence.AuditEntityUser+" "+trimmedId+" did not complete",
            exceptioncontract.Context{"entity": persistence.AuditEntityUser, "operation": "update", "id": trimmedId},
            txErr,
        )
    }

    return beforeAccount, afterAccount, nil
}

/* lockUserRow reads the account's row FOR UPDATE inside the caller's transaction; an absent row is an answer, not an error. */
func lockUserRow(ctx context.Context, tx bun.Tx, id string) (*userRow, bool, error) {
    row := &userRow{Id: id}

    selectErr := tx.NewSelect().Model(row).WherePK().For("UPDATE").Scan(ctx)
    if true == errors.Is(selectErr, sql.ErrNoRows) {
        return nil, false, nil
    }
    if nil != selectErr {
        return nil, false, selectErr
    }

    return row, true, nil
}

/* GrantRole reads the row locked FOR UPDATE and writes the widened set in the same transaction, so a grant and an admin update of the same account serialise on the row; the audit entry is recorded through the same transaction. */
func (instance *bunUserRepository) GrantRole(ctx context.Context, id string, role string) (*entity.User, GrantRoleOutcome, error) {
    trimmedId := strings.TrimSpace(id)
    if "" == trimmedId {
        return nil, GrantRoleAccountAbsent, fmt.Errorf("id is required")
    }

    outcome := GrantRoleAccountAbsent
    var account *entity.User

    txErr := instance.database.RunInTx(auditContext(ctx), nil, func(ctx context.Context, tx bun.Tx) error {
        before, found, lockErr := lockUserRow(ctx, tx, trimmedId)
        if nil != lockErr || false == found {
            return lockErr
        }

        current := before.toEntity()
        if true == holdsRole(current.Roles, role) {
            outcome = GrantRoleAlreadyHeld
            account = current

            return nil
        }

        after := newUserRow(entity.NewUser(
            current.Id,
            current.Username,
            current.Password,
            append(append([]string{}, current.Roles...), role),
        ))

        if _, updateErr := tx.NewUpdate().Model(after).WherePK().Exec(ctx); nil != updateErr {
            return updateErr
        }

        recordErr := instance.recorder.RecordUpdate(
            melodyaudit.WithDatabase(ctx, tx),
            persistence.AuditEntityUser,
            trimmedId,
            before,
            after,
        )
        if nil != recordErr {
            return recordErr
        }

        /* the account answered is the one the transaction wrote, not a read after the commit that an admin door may have changed or deleted in between */
        outcome = GrantRoleGranted
        account = after.toEntity()

        return nil
    })
    if nil != txErr {
        /* the driver answers bare text inside the transaction, so the failure is titled with the account and the write, with the driver error as its cause */
        return nil, GrantRoleAccountAbsent, exception.NewError(
            "granting a role did not complete on the "+persistence.AuditEntityUser+" "+trimmedId,
            exceptioncontract.Context{"entity": persistence.AuditEntityUser, "operation": "grant role", "id": trimmedId, "role": role},
            txErr,
        )
    }

    return account, outcome, nil
}

/* DeleteById reads the row locked FOR UPDATE, asks the guard, and removes it in the same transaction, the trail keeping the roles it held; a missing account is an answer, not an error. */
func (instance *bunUserRepository) DeleteById(ctx context.Context, id string, guard UserGuard) (*entity.User, error) {
    trimmedId := strings.TrimSpace(id)
    if "" == trimmedId {
        return nil, fmt.Errorf("id is required")
    }

    var removed *entity.User
    var refusal error

    txErr := instance.database.RunInTx(auditContext(ctx), nil, func(ctx context.Context, tx bun.Tx) error {
        before, found, lockErr := lockUserRow(ctx, tx, trimmedId)
        if nil != lockErr || false == found {
            return lockErr
        }

        current := before.toEntity()
        if refusal = admittedBy(guard, current); nil != refusal {
            return nil
        }

        if _, deleteErr := tx.NewDelete().Model(before).WherePK().Exec(ctx); nil != deleteErr {
            return deleteErr
        }

        recordErr := instance.recorder.RecordDelete(melodyaudit.WithDatabase(ctx, tx), persistence.AuditEntityUser, trimmedId, before)
        if nil != recordErr {
            return recordErr
        }

        removed = current

        return nil
    })
    if nil != refusal {
        return nil, refusal
    }

    if nil != txErr {
        return nil, exception.NewError(
            "deleting the "+persistence.AuditEntityUser+" "+trimmedId+" did not complete",
            exceptioncontract.Context{"entity": persistence.AuditEntityUser, "operation": "delete", "id": trimmedId},
            txErr,
        )
    }

    return removed, nil
}

/* ErrUsernameAlreadyExists is the refusal both write doors answer for a name another account holds, whether the preceding read or the unique index caught it, so the http doors answer 400 rather than 500. */
var ErrUsernameAlreadyExists = errors.New("username already exists")

/* asUsernameAlreadyExists maps the unique index's refusal onto ErrUsernameAlreadyExists: the read before the write cannot stop two concurrent callers, and the index is what holds the name. The refusal is matched on the index's own name, looked for down the whole chain of causes because the audit tracker wraps the driver's error; any other failure is answered untouched. */
func asUsernameAlreadyExists(writeErr error) error {
    if nil == writeErr {
        return nil
    }

    if false == errorChainNamesKey(writeErr, migration.UserUsernameIndexName) {
        return writeErr
    }

    return ErrUsernameAlreadyExists
}

/* errorChainNamesKey answers whether any link of the chain, every branch of a joined error included, is the driver's duplicate refusal for the named index, read from the refusal's key clause rather than searched for in the text. The walk visits at most errorChainLinkLimit links across all branches, so a chain that closes on itself ends instead of exhausting the stack. */
func errorChainNamesKey(err error, indexName string) bool {
    return errorChainHolds(err, func(text string) bool {
        return duplicateRefusalNamesKey(text, indexName)
    })
}

/* errorChainHolds answers whether any link of the chain, every branch of a joined error included, has a text the predicate accepts, visiting at most errorChainLinkLimit links */
func errorChainHolds(err error, holds func(text string) bool) bool {
    remainingLinks := errorChainLinkLimit

    var walk func(link error) bool
    walk = func(link error) bool {
        if nil == link || 0 == remainingLinks {
            return false
        }
        remainingLinks--

        if true == holds(link.Error()) {
            return true
        }

        if joined, isJoined := link.(interface{ Unwrap() []error }); true == isJoined {
            for _, branch := range joined.Unwrap() {
                if true == walk(branch) {
                    return true
                }
            }

            return false
        }

        return walk(errors.Unwrap(link))
    }

    return walk(err)
}

/* errorChainLinkLimit is far past any chain a write produces and small enough that a cyclic one ends at once. */
const errorChainLinkLimit = 64

/* duplicateRefusalNamesKey reads the key clause of a MySQL duplicate refusal, "for key '<table>.<index>'", and answers whether it names the index given, bare or qualified. The clause read is the last one, because the duplicated value is rendered unescaped before it and may spell a clause itself. */
func duplicateRefusalNamesKey(text string, indexName string) bool {
    const keyClause = "for key '"

    clauseStart := strings.LastIndex(text, keyClause)
    if -1 == clauseStart {
        return false
    }

    key := text[clauseStart+len(keyClause):]
    keyEnd := strings.Index(key, "'")
    if -1 == keyEnd {
        return false
    }
    key = key[:keyEnd]

    return key == indexName || true == strings.HasSuffix(key, "."+indexName)
}

func usernameTakenByAnother(ctx context.Context, database bun.IDB, username string, excludedId string) (bool, error) {
    wanted := NormalizedUsername(username)
    if "" == wanted {
        return false, nil
    }

    count, countErr := usernameTakenByAnotherQuery(database, wanted, excludedId).Count(ctx)
    if nil != countErr {
        return false, countErr
    }

    return 0 < count, nil
}

/* the lookup compares the column the application folded, under its binary collation, so it admits exactly the spellings NormalizedUsername makes one: the cache keys and the invalidation listeners fold through the same function */
func (instance *bunUserRepository) userByUsernameQuery(row *userRow, wanted string) *bun.SelectQuery {
    return instance.database.
        NewSelect().
        Model(row).
        Where("username_normalized = ?", wanted).
        Limit(1)
}

/* the same column as userByUsernameQuery, so the uniqueness door and the lookup door admit the same spellings */
func usernameTakenByAnotherQuery(database bun.IDB, wanted string, excludedId string) *bun.SelectQuery {
    return database.
        NewSelect().
        Model((*userRow)(nil)).
        Where("username_normalized = ?", wanted).
        Where("id != ?", excludedId)
}

func (instance *bunUserRepository) identifierList(ctx context.Context) ([]string, error) {
    identifierList := make([]string, 0)

    selectErr := instance.database.
        NewSelect().
        Model((*userRow)(nil)).
        Column("id").
        Scan(ctx, &identifierList)
    if nil != selectErr {
        return nil, selectErr
    }

    return identifierList, nil
}

var _ UserRepository = (*bunUserRepository)(nil)
