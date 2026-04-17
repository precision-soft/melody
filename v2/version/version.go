package version

/** @important version is overridden at build time using -ldflags */
var buildVersion = "v2.5.0"

func BuildVersion() string {
    return buildVersion
}
