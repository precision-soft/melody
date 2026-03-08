package version

/** @important version is overridden at build time using -ldflags */
var buildVersion = "dev"

func BuildVersion() string {
    return buildVersion
}
