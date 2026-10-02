package cli

import (
    "bufio"
    "errors"
    "fmt"
    "io"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/security"
    "github.com/precision-soft/melody/v3/.example/service"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* UserCreateCommand creates an account from the console: the door the first administrator of a deployment comes through, since outside development the directory starts empty. The username is its one argument and the role its required --role flag, spelt as example:grant:role spells it; the password is read from the first line of its input — standard input in the wiring — and never from an argument, where the shell history and the process list would keep it. The account is written through the user service, so the trail records it and the created event runs its listeners, as the admin create door does. */
type UserCreateCommand struct {
    userService   *melodycontainer.LazyService[*service.UserService]
    passwordInput io.Reader
}

func NewUserCreateCommand(userService *melodycontainer.LazyService[*service.UserService], passwordInput io.Reader) *UserCreateCommand {
    return &UserCreateCommand{userService: userService, passwordInput: passwordInput}
}

func (instance *UserCreateCommand) Name() string {
    return "example:user:create"
}

func (instance *UserCreateCommand) Description() string {
    return "creates the account named as the argument with the --role it is given; the password is read from standard input, never from an argument"
}

func (instance *UserCreateCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{
        &melodyclicontract.StringFlag{
            Name:     "role",
            Usage:    "the application role the account holds",
            Required: true,
            Aliases:  []string{"r"},
        },
    }
}

func (instance *UserCreateCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext melodyclicontract.Context) error {
    writer := commandContext.Writer()

    arguments := commandContext.Arguments()
    if 1 < len(arguments) {
        return errors.New("name only the account as the argument: the password is read from standard input, never from an argument, where the shell history and the process list keep it")
    }

    if 1 != len(arguments) {
        return errors.New("name the account to create as the argument")
    }

    username := strings.TrimSpace(arguments[0])
    if "" == username {
        return errors.New("the username is required")
    }

    if false == service.CacheSafeIdentifier(repository.NormalizedUsername(username)) {
        return errors.New("the username must stay within 255 bytes")
    }

    role := strings.TrimSpace(commandContext.String("role"))
    if true == strings.Contains(role, ",") {
        return fmt.Errorf("role %q must not contain commas", role)
    }

    if false == entity.IsKnownRole(role) {
        return fmt.Errorf("role %q is not one this application knows (%s)", role, strings.Join(entity.KnownRoleList(), ", "))
    }

    password, readErr := instance.readPassword()
    if nil != readErr {
        return readErr
    }

    passwordHash, hashErr := security.HashPassword(password)
    if nil != hashErr {
        return hashErr
    }

    userService, resolveErr := instance.userService.Resolve()
    if nil != resolveErr {
        return resolveErr
    }

    user, createErr := userService.Create(runtimeInstance, "", username, passwordHash, []string{role})
    if nil != createErr {
        if true == errors.Is(createErr, repository.ErrUsernameAlreadyExists) {
            return fmt.Errorf("user %q already exists", username)
        }

        return createErr
    }

    _, _ = fmt.Fprintf(writer, "created user %q (%s) holding role %q\n", user.Username, user.Id, role)

    return nil
}

/* readPassword reads the first line of the command's input, trimmed as the admin create door trims a password, and refuses an empty one or one past what bcrypt reads */
func (instance *UserCreateCommand) readPassword() (string, error) {
    if nil == instance.passwordInput {
        return "", errors.New("the password is read from standard input, and this command has none")
    }

    line, readErr := bufio.NewReader(instance.passwordInput).ReadString('\n')
    if nil != readErr && false == errors.Is(readErr, io.EOF) {
        return "", fmt.Errorf("reading the password from standard input: %w", readErr)
    }

    password := strings.TrimSpace(line)
    if "" == password {
        return "", errors.New("the password is required, as the first line of standard input")
    }

    if security.PasswordMaximumBytes < len(password) {
        return "", errors.New(security.PasswordTooLongMessage)
    }

    return password, nil
}

var _ melodyclicontract.Command = (*UserCreateCommand)(nil)
