package repository

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v2/.example/entity"
    "github.com/precision-soft/melody/v2/.example/migration"
    "github.com/uptrace/bun"
)

/* userRow is the directory as the database holds it. Roles are stored as one comma-separated column rather than a second table: they are a short fixed vocabulary with no commas in it, and the example is a nomenclature rather than a lesson in normalisation. */
type userRow struct {
    bun.BaseModel `bun:"table:melody_example_v2_user,alias:example_user"`

    Id       string `bun:"id,pk"`
    Username string `bun:"username,notnull"`
    Password string `bun:"password,notnull"`
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

func NewBunUserRepository(database *bun.DB) *bunUserRepository {
    return &bunUserRepository{database: database}
}

/* userIdentifierMintLockName names the advisory lock the creates of melody_example_v2_user mint their identifiers under */
const userIdentifierMintLockName = "melody_example_v2_user.id"

type bunUserRepository struct {
    database *bun.DB
}

/* seedIfEmpty writes the opening directory into an empty table; the table itself belongs to the migration set the provider has already applied. The insert ignores duplicate keys because several example applications may reach an empty table at the same time, and losing that race is not a failure. */
func (instance *bunUserRepository) seedIfEmpty(ctx context.Context) error {
    count, countErr := instance.database.
        NewSelect().
        Model((*userRow)(nil)).
        Count(ctx)
    if nil != countErr {
        return countErr
    }

    if 0 < count {
        return nil
    }

    seedList := seedUserList()
    rowList := make([]*userRow, 0, len(seedList))
    for _, user := range seedList {
        rowList = append(rowList, newUserRow(user))
    }

    _, insertErr := instance.database.
        NewInsert().
        Model(&rowList).
        Ignore().
        Exec(ctx)

    return insertErr
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

    /* the comparison is forced onto the binary collation because the column's own folds accents, while NormalizedUsername, the one spelling the cache keys and the invalidation listeners agree on, folds case alone, so this door matches exactly the users the invalidation can address */
    selectErr := instance.database.
        NewSelect().
        Model(row).
        Where("LOWER(username) = (? COLLATE utf8mb4_bin)", wanted).
        Limit(1).
        Scan(ctx)
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
        /* a supplied id that is occupied is answered "id already exists" before the insert, as in the sibling repositories, rather than as the primary key's raw duplicate-key text */
        _, occupied, occupiedErr := instance.findRowById(ctx, user.Id)
        if nil != occupiedErr {
            return occupiedErr
        }

        if true == occupied {
            return ErrIdAlreadyExists
        }
    }

    return insertWithMintedIdentifier(
        ctx,
        instance.database,
        userIdentifierMintLockName,
        mintsIdentifier,
        func() error {
            identifierList, identifierErr := instance.identifierList(ctx)
            if nil != identifierErr {
                return identifierErr
            }

            user.Id = nextUserId(identifierList)

            return nil
        },
        func() error {
            _, insertErr := instance.database.
                NewInsert().
                Model(newUserRow(user)).
                Exec(ctx)

            return asUsernameAlreadyExists(insertErr)
        },
    )
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

    result, updateErr := instance.database.
        NewUpdate().
        Model(newUserRow(user)).
        WherePK().
        Exec(ctx)
    if nil != updateErr {
        return false, asUsernameAlreadyExists(updateErr)
    }

    if true == affectedAtLeastOneRow(result) {
        return true, nil
    }

    /* MySQL answers the rows an update changed, not the rows it matched, so an update writing the values the row already holds reports none: the row is read again, and only a row that is gone by now is answered as absent */
    _, stillFound, refindErr := instance.findRowById(ctx, id)

    return stillFound, refindErr
}

func (instance *bunUserRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    trimmedId := strings.TrimSpace(id)
    if "" == trimmedId {
        return false, fmt.Errorf("id is required")
    }

    result, deleteErr := instance.database.
        NewDelete().
        Model((*userRow)(nil)).
        Where("id = ?", trimmedId).
        Exec(ctx)
    if nil != deleteErr {
        return false, deleteErr
    }

    return affectedAtLeastOneRow(result), nil
}

func (instance *bunUserRepository) usernameTakenByAnother(ctx context.Context, username string, excludedId string) (bool, error) {
    wanted := NormalizedUsername(username)
    if "" == wanted {
        return false, nil
    }

    /* the same binary collation as FindByUsername, so the uniqueness door and the lookup door refuse and admit the exact same spellings */
    count, countErr := instance.database.
        NewSelect().
        Model((*userRow)(nil)).
        Where("LOWER(username) = (? COLLATE utf8mb4_bin)", wanted).
        Where("id != ?", excludedId).
        Count(ctx)
    if nil != countErr {
        return false, countErr
    }

    return 0 < count, nil
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

/* ErrUsernameAlreadyExists is the refusal both write doors answer for a name another account holds, whether the preceding read or the unique index caught it, so the http doors answer 400 rather than 500. */
var ErrUsernameAlreadyExists = errors.New("username already exists")

/* asUsernameAlreadyExists maps the unique index's refusal onto ErrUsernameAlreadyExists: the read before the write cannot stop two concurrent callers, and the index is what holds the name. The refusal is matched on the index's own name, looked for down the whole chain of causes; any other failure is answered untouched. */
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
