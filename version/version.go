package version

/* @important version is overridden at build time using -ldflags */
var buildVersion = "v1.18.1"

func BuildVersion() string {
    return buildVersion
}
