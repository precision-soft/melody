package contract

/* RoleHierarchy expands a list of roles with the roles they inherit. */
type RoleHierarchy interface {
    ExpandRoles(roles []string) []string
}

/* RoleHierarchyBinder is the optional capability of a token source that decides on roles while it resolves a token. Compile calls it once per firewall holding the source, with the firewall's effective hierarchy, nil when the firewall has none; a source that cannot serve the hierarchy it is handed answers the error, which fails the compile. A decorator around such a source forwards the call. */
type RoleHierarchyBinder interface {
    BindRoleHierarchy(firewallName string, hierarchy RoleHierarchy) error
}
