package version

/* @important version is overridden at build time using -ldflags */
var buildVersion = "v2.12.1"

func BuildVersion() string {
    return buildVersion
}
