package version

/* version is overridden at build time using -ldflags */
var buildVersion = "v3.14.0"

func BuildVersion() string {
    return buildVersion
}
