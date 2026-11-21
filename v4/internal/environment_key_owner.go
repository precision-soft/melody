package internal

/* the owners of a declared environment key that are not modules; a module may not take either name, or the key table would read its keys as theirs */
const (
    EnvironmentKeyOwnerFramework   = "the framework"
    EnvironmentKeyOwnerApplication = "the application"
)
