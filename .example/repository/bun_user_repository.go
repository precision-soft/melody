package repository

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/uptrace/bun"
)

/* userRow is the directory as the database holds it. Roles are stored as one comma-separated column rather than a second table: they are a short fixed vocabulary with no commas in it, and the example is a nomenclature rather than a lesson in normalisation. */
type userRow struct {
    bun.BaseModel `bun:"table:melody_example_v1_user,alias:example_user"`

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

    selectErr := instance.database.
        NewSelect().
        Model(row).
        Where("LOWER(username) = ?", wanted).
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

    _, insertErr := instance.database.
        NewInsert().
        Model(newUserRow(user)).
        Exec(ctx)

    return insertErr
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

    result, updateErr := instance.database.
        NewUpdate().
        Model(newUserRow(user)).
        WherePK().
        Exec(ctx)
    if nil != updateErr {
        return false, updateErr
    }

    return affectedAtLeastOneRow(result), nil
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

    count, countErr := instance.database.
        NewSelect().
        Model((*userRow)(nil)).
        Where("LOWER(username) = ?", wanted).
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
