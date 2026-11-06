package adminapi

import "runtime/debug"

const modulePath = "github.com/shopware/sdk-go/clients/adminapi"

// develVersion is the version Go records for a module built from local
// source: the main module, a go.work module or a replaced module.
const develVersion = "(devel)"

// fallbackVersion is reported when the build has no release version of this
// module.
const fallbackVersion = "dev"

// DefaultUserAgent is sent as User-Agent unless Config.UserAgent is set. It
// contains the module version when the build information has it.
var DefaultUserAgent = "shopware-sdk-go-adminapi/" + moduleVersion()

func moduleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return fallbackVersion
	}
	return versionFromBuildInfo(info)
}

// versionFromBuildInfo returns the version of this module from info, or
// "dev" when the module was built from local source or is not listed.
func versionFromBuildInfo(info *debug.BuildInfo) string {
	if info.Main.Path == modulePath && isReleaseVersion(info.Main.Version) {
		return info.Main.Version
	}
	for _, dep := range info.Deps {
		if dep.Path != modulePath {
			continue
		}
		// With a replace directive dep.Version is the required version, and
		// dep.Replace describes the code that was actually built: "(devel)"
		// for a local directory, or the version of the replacement module.
		version := dep.Version
		if dep.Replace != nil {
			version = dep.Replace.Version
		}
		if isReleaseVersion(version) {
			return version
		}
		return fallbackVersion
	}
	return fallbackVersion
}

func isReleaseVersion(version string) bool {
	return version != "" && version != develVersion
}
