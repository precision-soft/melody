package version

/* buildVersion is the release default, bumped per release; -ldflags overrides it at build time */
var buildVersion = "v1.19.4"

func BuildVersion() string {
    return buildVersion
}
