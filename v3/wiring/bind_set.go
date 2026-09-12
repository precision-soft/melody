package wiring

/* BindSet declares how the scalar arguments of the scanned constructors are filled. A constructor argument whose type is not a service — a string, a number, a duration — cannot be resolved from the container, so it is bound by argument name to a configuration parameter, the way an application binds its own settings once instead of threading them through every provider.

   A bind declared on a package applies only to the constructors found under it; a bind declared on the set itself applies everywhere. The generator reports a bind that never matched an argument, which is what turns a misspelled name into an error rather than a silently unfilled dependency. */
func NewBindSet() *BindSet {
    return &BindSet{
        packages:        make([]*PackageBinding, 0),
        globalBinds:     make(map[string]string),
        globalBindOrder: make([]string, 0),
    }
}

type BindSet struct {
    packages        []*PackageBinding
    globalBinds     map[string]string
    globalBindOrder []string
}

/* Package declares a package to scan. The import path is the one the generated file imports; the directory is where the sources live, relative to the project directory, and is scanned together with everything below it. */
func (instance *BindSet) Package(importPath string, directory string) *PackageBinding {
    packageBinding := &PackageBinding{
        importPath: importPath,
        directory:  directory,
        binds:      make(map[string]string),
        bindOrder:  make([]string, 0),
        excludes:   make([]string, 0),
    }

    instance.packages = append(instance.packages, packageBinding)

    return packageBinding
}

/* Name binds an argument name to a configuration parameter across every scanned package. */
func (instance *BindSet) Name(argumentName string, parameterName string) *BindSet {
    if _, exists := instance.globalBinds[argumentName]; false == exists {
        instance.globalBindOrder = append(instance.globalBindOrder, argumentName)
    }

    instance.globalBinds[argumentName] = parameterName

    return instance
}

func (instance *BindSet) Packages() []*PackageBinding {
    packages := make([]*PackageBinding, len(instance.packages))

    copy(packages, instance.packages)

    return packages
}

func (instance *BindSet) GlobalBindNames() []string {
    names := make([]string, len(instance.globalBindOrder))

    copy(names, instance.globalBindOrder)

    return names
}

func (instance *BindSet) GlobalBind(argumentName string) (string, bool) {
    parameterName, exists := instance.globalBinds[argumentName]

    return parameterName, exists
}

type PackageBinding struct {
    importPath string
    directory  string
    binds      map[string]string
    bindOrder  []string
    excludes   []string
}

/* Name binds an argument name to a configuration parameter for the constructors of this package only, taking precedence over a bind of the same name declared on the set. */
func (instance *PackageBinding) Name(argumentName string, parameterName string) *PackageBinding {
    if _, exists := instance.binds[argumentName]; false == exists {
        instance.bindOrder = append(instance.bindOrder, argumentName)
    }

    instance.binds[argumentName] = parameterName

    return instance
}

/* Exclude skips every constructor whose returned type name matches the pattern, in the syntax of path.Match. Test files are always excluded. */
func (instance *PackageBinding) Exclude(pattern string) *PackageBinding {
    instance.excludes = append(instance.excludes, pattern)

    return instance
}

func (instance *PackageBinding) ImportPath() string {
    return instance.importPath
}

func (instance *PackageBinding) Directory() string {
    return instance.directory
}

func (instance *PackageBinding) Excludes() []string {
    excludes := make([]string, len(instance.excludes))

    copy(excludes, instance.excludes)

    return excludes
}

func (instance *PackageBinding) BindNames() []string {
    names := make([]string, len(instance.bindOrder))

    copy(names, instance.bindOrder)

    return names
}

func (instance *PackageBinding) Bind(argumentName string) (string, bool) {
    parameterName, exists := instance.binds[argumentName]

    return parameterName, exists
}
