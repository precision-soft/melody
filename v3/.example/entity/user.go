package entity

const (
    RoleUser   = "ROLE_USER"
    RoleEditor = "ROLE_EDITOR"
    RoleAdmin  = "ROLE_ADMIN"
)

/* KnownRoleList is the closed vocabulary of roles this application grants, the one list every door that
   writes a role reads: the voter compares a role's spelling exactly, so a spelling outside this list is
   stored and grants nothing. */
func KnownRoleList() []string {
    return []string{RoleUser, RoleEditor, RoleAdmin}
}

func IsKnownRole(role string) bool {
    for _, known := range KnownRoleList() {
        if known == role {
            return true
        }
    }

    return false
}

func NewUser(
    id string,
    username string,
    password string,
    roles []string,
) *User {
    return &User{
        Id:       id,
        Username: username,
        Password: password,
        Roles:    roles,
    }
}

type User struct {
    Id       string
    Username string
    Password string
    Roles    []string
}
