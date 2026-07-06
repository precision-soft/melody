package version

/* @important version is overridden at build time using -ldflags */
var buildVersion = "v1.15.0"

func BuildVersion() string {
    return buildVersion
}
