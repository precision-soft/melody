package wiring

/* BindSet declares how the scalar arguments of the scanned constructors are filled: an argument whose type is not a service is bound by name to a configuration parameter. A bind declared on a package applies to the constructors under it, one declared on the set everywhere; the generator reports a bind that never matched an argument. */
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
}

/* Name binds an argument name to a configuration parameter for the constructors of this package only, taking precedence over a bind of the same name declared on the set. */
func (instance *PackageBinding) Name(argumentName string, parameterName string) *PackageBinding {
    if _, exists := instance.binds[argumentName]; false == exists {
        instance.bindOrder = append(instance.bindOrder, argumentName)
    }

    instance.binds[argumentName] = parameterName

    return instance
}

func (instance *PackageBinding) ImportPath() string {
    return instance.importPath
}

func (instance *PackageBinding) Directory() string {
    return instance.directory
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
