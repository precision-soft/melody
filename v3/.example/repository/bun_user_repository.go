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
    /* the trail records that the password changed and never its values: a history of credentials is what an audit trail must not become */
    Password string `bun:"password,notnull" audit:"redact"`
    Roles    string `bun:"roles,notnull"`
}

func newUserRow(user *entity.User) *userRow {
    return &userRow{
        Id:       user.Id,
        Username: user.Username,
        Password: user.Password,
        Roles:    strings.Join(user.Roles, ","),
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
    return &bunUserRepository{database: storage.Database(), tracker: storage.Tracker(), recorder: storage.Recorder()}
}

/* bunUserRepository keeps the directory in the database and its history beside it. Every write goes through the audit tracker, the password recorded as changed without its value; GrantRole, which runs its own transaction, records through the recorder inside it. */
type bunUserRepository struct {
    database *bun.DB
    tracker  *melodyaudit.Tracker
    recorder *melodyaudit.Recorder
}

func (instance *bunUserRepository) seedIfEmpty(ctx context.Context) error {
    return seedIfEmptyRows(ctx, instance.database, func() []*userRow {
        seedList := seedUserList()
        rowList := make([]*userRow, 0, len(seedList))
        for _, user := range seedList {
            rowList = append(rowList, newUserRow(user))
        }

        return rowList
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

    if "" == strings.TrimSpace(user.Id) {
        identifierList, identifierErr := instance.identifierList(ctx)
        if nil != identifierErr {
            return identifierErr
        }

        user.Id = nextUserId(identifierList)
    }

    /* an occupied id is answered "id already exists" before the insert, as in the sibling repositories, rather than as the primary key's raw duplicate-key text */
    _, occupied, occupiedErr := instance.findRowById(ctx, user.Id)
    if nil != occupiedErr {
        return occupiedErr
    }

    if true == occupied {
        return fmt.Errorf("id already exists")
    }

    insertErr := instance.tracker.Insert(auditContext(ctx), persistence.AuditEntityUser, user.Id, newUserRow(user))

    return asUsernameAlreadyExists(insertErr)
}

func (instance *bunUserRepository) Update(ctx context.Context, user *entity.User) (bool, error) {
    validationErr := validateUser(user)
    if nil != validationErr {
        return false, validationErr
    }

    id := strings.TrimSpace(user.Id)
    if "" == id {
        return false, fmt.Errorf("id is required")
    }

    _, found, findErr := instance.findRowById(ctx, id)
    if nil != findErr {
        return false, findErr
    }

    if false == found {
        return false, nil
    }

    takenByAnother, takenErr := instance.usernameTakenByAnother(ctx, user.Username, id)
    if nil != takenErr {
        return false, takenErr
    }

    if true == takenByAnother {
        return false, ErrUsernameAlreadyExists
    }

    updateErr := instance.tracker.Update(auditContext(ctx), persistence.AuditEntityUser, id, newUserRow(user))
    if nil != updateErr {
        return false, asUsernameAlreadyExists(updateErr)
    }

    return true, nil
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
        before := &userRow{Id: trimmedId}

        selectErr := tx.NewSelect().Model(before).WherePK().For("UPDATE").Scan(ctx)
        if true == errors.Is(selectErr, sql.ErrNoRows) {
            return nil
        }
        if nil != selectErr {
            return selectErr
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

func (instance *bunUserRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    trimmedId := strings.TrimSpace(id)
    if "" == trimmedId {
        return false, fmt.Errorf("id is required")
    }

    /* the account is opted into a captured before-image, so the tracker locks the row before removing it and the trail keeps the roles it held; a missing account is an answer, not an error */
    _, found, findErr := instance.findRowById(ctx, trimmedId)
    if nil != findErr {
        return false, findErr
    }

    if false == found {
        return false, nil
    }

    deleteErr := instance.tracker.Delete(
        auditContext(ctx),
        persistence.AuditEntityUser,
        trimmedId,
        &userRow{Id: trimmedId},
    )
    if nil != deleteErr {
        return false, deleteErr
    }

    return true, nil
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
    remainingLinks := errorChainLinkLimit

    var walk func(link error) bool
    walk = func(link error) bool {
        if nil == link || 0 == remainingLinks {
            return false
        }
        remainingLinks--

        if true == duplicateRefusalNamesKey(link.Error(), indexName) {
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

func (instance *bunUserRepository) usernameTakenByAnother(ctx context.Context, username string, excludedId string) (bool, error) {
    wanted := NormalizedUsername(username)
    if "" == wanted {
        return false, nil
    }

    count, countErr := instance.usernameTakenByAnotherQuery(wanted, excludedId).Count(ctx)
    if nil != countErr {
        return false, countErr
    }

    return 0 < count, nil
}

/* the comparison is forced onto the binary collation because the column's own folds accents while NormalizedUsername, the spelling the cache keys and the invalidation listeners share, folds case alone; under the column's collation this door would match users the invalidation cannot address */
func (instance *bunUserRepository) userByUsernameQuery(row *userRow, wanted string) *bun.SelectQuery {
    return instance.database.
        NewSelect().
        Model(row).
        Where("LOWER(username) = (? COLLATE utf8mb4_bin)", wanted).
        Limit(1)
}

/* the same binary collation as userByUsernameQuery, so the uniqueness door and the lookup door admit the same spellings */
func (instance *bunUserRepository) usernameTakenByAnotherQuery(wanted string, excludedId string) *bun.SelectQuery {
    return instance.database.
        NewSelect().
        Model((*userRow)(nil)).
        Where("LOWER(username) = (? COLLATE utf8mb4_bin)", wanted).
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
