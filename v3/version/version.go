package version

/** @important version is overridden at build time using -ldflags */
var buildVersion = "v3.4.0"

func BuildVersion() string {
    return buildVersion
}
