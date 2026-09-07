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
    "github.com/uptrace/bun"
)

/* userRow is the directory as the database holds it. Roles are stored as one comma-separated column rather than a second table: they are a short fixed vocabulary with no commas in it, and the example is a nomenclature rather than a lesson in normalisation. */
type userRow struct {
    bun.BaseModel `bun:"table:melody_example_v3_user,alias:example_user"`

    Id       string `bun:"id,pk"`
    Username string `bun:"username,notnull"`
    /* the trail records THAT the password changed and never what it changed to or from: a history of credentials is the one thing an audit trail must not become, and a reader of the trail has no business the plaintext would serve */
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
    return &bunUserRepository{database: storage.Database(), tracker: storage.Tracker()}
}

/* bunUserRepository keeps the directory in the database and its history beside it. Every write goes through the audit tracker, so who was granted which role, and when, is answerable after the fact — and the password column is recorded as changed without its value ever entering the trail. */
type bunUserRepository struct {
    database *bun.DB
    tracker  *melodyaudit.Tracker
}

/* bcryptDigestPattern matches a stored password that bcrypt can read: every digest it writes opens with the
   version marker. A value that does not is one this application stored before it moved to bcrypt. */
const bcryptDigestPattern = "$%"

/* repairLegacySeedPasswords rewrites the opening directory's passwords when a database provisioned before
   this application moved to bcrypt still holds them in the digest it used then.

   Without it the switch locks those accounts out for good: bcrypt refuses a value that is not one of its
   digests, seedIfEmpty writes nothing into a table that already has rows, and the only doors that could set
   a new password are behind the administrator account that is itself locked out — so the readme's
   `admin` / `admin` would be a promise no volume older than this change could keep.

   Only the three accounts this application seeded are rewritten, and only while they hold a value bcrypt
   cannot read: an account an operator created carries a password this application never knew and cannot
   reproduce, so it keeps its row and is reset through the administrator door, which works again. The write
   goes to the column directly rather than through the audit tracker: the trail answers who changed what,
   and this is the application repairing its own opening data, not a person changing a credential. */
func (instance *bunUserRepository) repairLegacySeedPasswords(ctx context.Context) error {
    legacyCount, countErr := instance.database.
        NewSelect().
        Model((*userRow)(nil)).
        Where("password NOT LIKE ?", bcryptDigestPattern).
        Count(ctx)
    if nil != countErr {
        return countErr
    }

    /* the ordinary case, on every boot after the first: nothing to read and nothing to write */
    if 0 == legacyCount {
        return nil
    }

    for _, user := range seedUserList() {
        _, updateErr := instance.legacyPasswordUpdate(user).Exec(ctx)
        if nil != updateErr {
            return updateErr
        }
    }

    return nil
}

/* legacyPasswordUpdate is the one statement the repair issues per seeded account, kept as a query so the
   guard that decides which rows it may touch is readable on its own: the row is this account's, and its
   stored password is one bcrypt cannot read. Without the second clause the repair would reset a password an
   administrator had already changed, every time a process booted. */
func (instance *bunUserRepository) legacyPasswordUpdate(user *entity.User) *bun.UpdateQuery {
    return instance.database.
        NewUpdate().
        Model((*userRow)(nil)).
        Set("password = ?", user.Password).
        Where("id = ?", user.Id).
        Where("password NOT LIKE ?", bcryptDigestPattern)
}

/* seedIfEmpty writes the opening directory into an empty table; the table itself belongs to the migration set the constructor has already applied. The insert ignores duplicate keys because several example applications may reach an empty table at the same time, and losing that race is not a failure. */
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

    selectErr := instance.userByUsernameQuery(row, wanted).Scan(ctx)
    if nil != selectErr {
        if true == errors.Is(selectErr, sql.ErrNoRows) {
            return nil, false, nil
        }

        return nil, false, selectErr
    }

    return row.toEntity(), true, nil
}

/* findRowById separates a row that is not there from a query that could not run: only sql.ErrNoRows is an answer, and every other failure is reported. */
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
        return fmt.Errorf("username already exists")
    }

    if "" == strings.TrimSpace(user.Id) {
        identifierList, identifierErr := instance.identifierList(ctx)
        if nil != identifierErr {
            return identifierErr
        }

        user.Id = nextUserId(identifierList)
    }

    /* the same guard the product, category and currency repositories carry, and the one the identifier
       ceiling's own rationale promises: without it an occupied id reaches the insert, where the primary
       key answers the driver's raw duplicate-key text through a 500, and two callers that mint the same
       id concurrently — the ordinary case, since the mint reads a list that neither has committed to
       yet — see that instead of "id already exists". */
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
        return false, fmt.Errorf("username already exists")
    }

    updateErr := instance.tracker.Update(auditContext(ctx), persistence.AuditEntityUser, id, newUserRow(user))
    if nil != updateErr {
        return false, asUsernameAlreadyExists(updateErr)
    }

    return true, nil
}

func (instance *bunUserRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    trimmedId := strings.TrimSpace(id)
    if "" == trimmedId {
        return false, fmt.Errorf("id is required")
    }

    /* the account is opted into a captured before-image, so the tracker loads and locks the row before removing it and the trail keeps which roles it held; an account that is not there is not an error, it is an answer the caller asked for */
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

/* the check that precedes the write is a read, so two callers can both pass it before either has written;
   the unique index the migration set adds is what actually holds the name, and this is where its refusal
   is given the message the door already answers when the check catches the name in time. The match is on
   the index's own name — this application's identifier, not the driver's wording — because the driver
   spells the refusal as `Duplicate entry '<value>' for key '<table>.<index>'`, measured on the running
   server; any other failure is handed back untouched, so a duplicate on the primary key stays the
   diagnosis it is rather than being reported as a name that is taken. */
func asUsernameAlreadyExists(writeErr error) error {
    if nil == writeErr {
        return nil
    }

    if false == strings.Contains(writeErr.Error(), migration.UserUsernameIndexName) {
        return writeErr
    }

    return fmt.Errorf("username already exists")
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

/* the comparison is forced onto the binary collation because the column's own (utf8mb4_0900_ai_ci) folds accents — 'café' = 'cafe' is true under it — while NormalizedUsername, the one spelling the cache keys and the invalidation listeners agree on, folds case alone; left to the column, this door matched users the invalidation could never address, and a deleted user kept authenticating from the ttl-less cache under the collation-only spelling.

   Both doors are kept as queries, the way legacyPasswordUpdate is, so the clause that decides which rows they may match is readable — and provable — on its own. */
func (instance *bunUserRepository) userByUsernameQuery(row *userRow, wanted string) *bun.SelectQuery {
    return instance.database.
        NewSelect().
        Model(row).
        Where("LOWER(username) = (? COLLATE utf8mb4_bin)", wanted).
        Limit(1)
}

/* the same binary collation as userByUsernameQuery, so the uniqueness door and the lookup door refuse and admit the exact same spellings */
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
