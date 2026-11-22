package config

import (
    "context"
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/security"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

func impersonationRuntime(t *testing.T, userRepository repository.UserRepository) melodycontainercontract.Runtime {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    melodycontainer.MustRegister[repository.UserRepository](containerInstance, repository.ServiceUserRepository, func(resolver melodycontainercontract.Resolver) (repository.UserRepository, error) {
        return userRepository, nil
    }, melodycontainer.RegisterOptions{})

    return melodyruntime.NewRun(context.Background(), containerInstance.NewScope())
}

func seededUserRepository(t *testing.T) repository.UserRepository {
    t.Helper()

    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil).WithAccountSeed(), &persistence.CatalogSchema{})
    if nil != repositoryErr {
        t.Fatalf("new user repository: %v", repositoryErr)
    }

    return userRepository
}

type failingUserRepository struct {
    repository.UserRepository
}


func resolver() *directoryImpersonatedUserResolver {
    return &directoryImpersonatedUserResolver{roleHierarchy: security.NewRoleHierarchy()}
}

func (instance failingUserRepository) FindById(context.Context, string) (*entity.User, bool, error) {
    return nil, false, errors.New("the directory is down")
}

func TestDirectoryImpersonatedUserResolver_SwitchesToAnAccountByItsIdWithItsStoredRoles(t *testing.T) {
    token, resolveErr := resolver().ResolveImpersonatedUser(impersonationRuntime(t, seededUserRepository(t)), "user-2")
    if nil != resolveErr || nil == token {
        t.Fatalf("expected the editor resolved, got %v %v", token, resolveErr)
    }

    if "user-2" != token.UserIdentifier() || 1 != len(token.Roles()) || entity.RoleEditor != token.Roles()[0] {
        t.Fatalf("expected the account's id and stored roles, got %q %v", token.UserIdentifier(), token.Roles())
    }
}

func TestDirectoryImpersonatedUserResolver_DeniesWhatItMayNotTakeOn(t *testing.T) {
    userRepository := seededUserRepository(t)
    for _, account := range []*entity.User{
        entity.NewUser("user-9", "switcher", "digest", []string{melodysecuritycontract.RoleAllowedToSwitch}),
        entity.NewUser("user-10", "roleless", "digest", []string{}),
    } {
        if createErr := userRepository.Create(context.Background(), account); nil != createErr {
            t.Fatalf("create %s: %v", account.Id, createErr)
        }
    }

    for _, identifier := range []string{"nobody", "editor", "user-3", "user-9", "user-10", "", strings.Repeat("u", 300), "user-\xff"} {
        token, resolveErr := resolver().ResolveImpersonatedUser(impersonationRuntime(t, userRepository), identifier)
        if nil != resolveErr || nil != token {
            t.Fatalf("%q: expected the switch denied with a nil token, got %v %v", identifier, token, resolveErr)
        }
    }
}

func TestDirectoryImpersonatedUserResolver_ReadsTheRolesThroughTheHierarchy(t *testing.T) {
    elevated := &directoryImpersonatedUserResolver{roleHierarchy: melodysecurity.NewRoleHierarchy(map[string][]string{entity.RoleEditor: {entity.RoleAdmin}})}

    if token, _ := elevated.ResolveImpersonatedUser(impersonationRuntime(t, seededUserRepository(t)), "user-2"); nil != token {
        t.Fatal("expected an account reaching the admin role through the hierarchy denied")
    }
}

func TestDirectoryImpersonatedUserResolver_ADirectoryThatCannotAnswerFailsClosedAsAnOutage(t *testing.T) {
    token, resolveErr := resolver().ResolveImpersonatedUser(impersonationRuntime(t, failingUserRepository{}), "user-2")
    if nil != token || false == melodyexception.IsInfrastructureFailure(resolveErr) {
        t.Fatalf("expected no token and the outage marked, got %v %v", token, resolveErr)
    }

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    withoutDirectory := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())
    if token, resolveErr := resolver().ResolveImpersonatedUser(withoutDirectory, "user-2"); nil != token || false == melodyexception.IsInfrastructureFailure(resolveErr) {
        t.Fatalf("expected a directory the container cannot resolve marked as an outage, got %v %v", token, resolveErr)
    }
}

type countingUserRepository struct {
    repository.UserRepository
    lookups int
}

func (instance *countingUserRepository) FindById(ctx context.Context, id string) (*entity.User, bool, error) {
    instance.lookups++

    return instance.UserRepository.FindById(ctx, id)
}

func TestDirectoryImpersonatedUserResolver_AnIdentifierTheSignInRefusesNeverReachesTheDirectory(t *testing.T) {
    counting := &countingUserRepository{UserRepository: seededUserRepository(t)}

    for _, identifier := range []string{"", strings.Repeat("u", 300), "user-\xff"} {
        resolver().ResolveImpersonatedUser(impersonationRuntime(t, counting), identifier)
    }

    if 0 != counting.lookups {
        t.Fatalf("expected no directory lookup for a refused identifier, got %d", counting.lookups)
    }
}
