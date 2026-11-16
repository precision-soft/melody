package config

import (
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/security"
    "github.com/precision-soft/melody/v4/.example/service"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* the impersonation wiring lets a caller holding the switch role act as another account on the token-protected /secure api by sending an X-Switch-User header naming the account id, the identifier every token of the example carries; the resolver reads that account from the user directory the sign-in reads */
func (instance *Module) buildImpersonation() {
    instance.impersonatedUsers = &directoryImpersonatedUserResolver{roleHierarchy: security.NewRoleHierarchy()}
}

type directoryImpersonatedUserResolver struct {
    roleHierarchy *melodysecurity.RoleHierarchy
}

/* ResolveImpersonatedUser denies, with a nil token, an identifier the sign-in would refuse, an id the directory does not hold, an account its own session would not honour (no password, no role) and an account that could itself administer or switch, read through the role hierarchy: the switch role reaches every identity this resolver answers, so taking on such an account would carry its rights. A directory that cannot answer fails closed as an outage. */
func (instance *directoryImpersonatedUserResolver) ResolveImpersonatedUser(
    runtimeInstance melodyruntimecontract.Runtime,
    identifier string,
) (melodysecuritycontract.Token, error) {
    if false == service.CacheSafeIdentifier(identifier) || false == utf8.ValidString(identifier) {
        return nil, nil
    }

    userRepository, resolveErr := melodycontainer.FromResolver[repository.UserRepository](runtimeInstance.Context(), runtimeInstance.Container(), repository.ServiceUserRepository)
    if nil != resolveErr {
        return nil, melodyexception.MarkInfrastructureFailure(resolveErr)
    }

    account, found, findErr := userRepository.FindById(runtimeInstance.Context(), identifier)
    if nil != findErr {
        return nil, melodyexception.MarkInfrastructureFailure(findErr)
    }

    if false == found || nil == account || "" == account.Password || 0 == len(account.Roles) {
        return nil, nil
    }

    expanded := instance.roleHierarchy.ExpandRoles(account.Roles)
    if true == security.HoldsRole(expanded, melodysecuritycontract.RoleAllowedToSwitch) || true == security.HoldsRole(expanded, entity.RoleAdmin) {
        return nil, nil
    }

    return melodysecurity.NewAuthenticatedToken(account.Id, append([]string{}, account.Roles...), nil), nil
}
