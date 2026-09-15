package repository

import (
    "context"
    driver "github.com/go-sql-driver/mysql"
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

type userRow struct {
    bun.BaseModel `bun:"table:melody_example_v3_user,alias:example_user"`

    Id       string `bun:"id,pk"`
    Username string `bun:"username,notnull"`
    /* Password audit records only that the credential changes; it omits both old and new secret values. */
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

type bunUserRepository struct {
    database *bun.DB
    tracker  *melodyaudit.Tracker
    recorder *melodyaudit.Recorder
}

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

func (instance *bunUserRepository) findRowById(ctx context.Context, id string) (*userRow, bool, error) {
    row := &userRow{}

    selectErr := instance.database.
        NewSelect().
        Model(row).
        Where("id = ? AND BINARY id = BINARY ?", id, id).
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

func asUsernameAlreadyExists(writeErr error) error {
    if nil == writeErr {
        return nil
    }

    var driverErr *driver.MySQLError
    if errors.As(writeErr, &driverErr) && nil != driverErr {
        if 1062 != driverErr.Number || false == strings.Contains(driverErr.Message, migration.UserUsernameIndexName) {
            return writeErr
        }
        return fmt.Errorf("username already exists")
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

func (instance *bunUserRepository) userByUsernameQuery(row *userRow, wanted string) *bun.SelectQuery {
    return instance.database.
        NewSelect().
        Model(row).
        Where("LOWER(username) = (? COLLATE utf8mb4_bin)", wanted).
        Limit(1)
}

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

/* GrantRole reads the current row under the same transaction that writes only its roles and records the audit. */
func (instance *bunUserRepository) GrantRole(ctx context.Context, username string, role string) (*entity.User, bool, error) {
    var account *entity.User
    changed := false
    err := instance.database.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
        before := &userRow{}
        err := tx.NewSelect().Model(before).Where("LOWER(username) = (? COLLATE utf8mb4_bin)", NormalizedUsername(username)).For("UPDATE").Scan(ctx)
        if errors.Is(err, sql.ErrNoRows) { return nil }
        if nil != err { return err }
        account = before.toEntity()
        for _, held := range account.Roles { if role == held { return nil } }
        account.Roles = append(account.Roles, role)
        after := newUserRow(account)
        if _, err := tx.NewUpdate().Model(after).Column("roles").WherePK().Where("BINARY id = BINARY ?", account.Id).Exec(ctx); nil != err { return err }
        if err := instance.recorder.RecordUpdate(melodyaudit.WithDatabase(auditContext(ctx), tx), persistence.AuditEntityUser, account.Id, before, after); nil != err { return err }
        changed = true
        return nil
    })
    if nil != err { return nil, false, err }
    return account, changed, nil
}
