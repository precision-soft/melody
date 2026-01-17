package entity

const (
	RoleUser   = "ROLE_USER"
	RoleEditor = "ROLE_EDITOR"
	RoleAdmin  = "ROLE_ADMIN"
)

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
