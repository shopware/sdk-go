package adminapi

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/shopware/app-sdk-go/clients/adminapi/internal/assert"
)

func TestVersionFromBuildInfo(t *testing.T) {
	dep := func(version string) []*debug.Module {
		return []*debug.Module{
			{Path: "golang.org/x/sync", Version: "v0.23.0"},
			{Path: modulePath, Version: version},
		}
	}
	tests := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"released dependency", &debug.BuildInfo{Main: debug.Module{Path: "example.com/app"}, Deps: dep("v1.0.0")}, "v1.0.0"},
		{"dependency from go.work or replace", &debug.BuildInfo{Main: debug.Module{Path: "example.com/app"}, Deps: dep("(devel)")}, "dev"},
		{"dependency without version", &debug.BuildInfo{Main: debug.Module{Path: "example.com/app"}, Deps: dep("")}, "dev"},
		{"not a dependency", &debug.BuildInfo{Main: debug.Module{Path: "example.com/app"}}, "dev"},
		{
			"replaced by a local directory",
			&debug.BuildInfo{Main: debug.Module{Path: "example.com/app"}, Deps: []*debug.Module{
				{Path: modulePath, Version: "v1.0.0", Replace: &debug.Module{Path: "../adminapi", Version: "(devel)"}},
			}},
			"dev",
		},
		{
			"replaced by another release",
			&debug.BuildInfo{Main: debug.Module{Path: "example.com/app"}, Deps: []*debug.Module{
				{Path: modulePath, Version: "v1.0.0", Replace: &debug.Module{Path: "example.com/fork/adminapi", Version: "v1.0.1"}},
			}},
			"v1.0.1",
		},
		{"main module released", &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "v1.2.3"}}, "v1.2.3"},
		{"main module from source", &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "(devel)"}}, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, versionFromBuildInfo(tt.info), tt.want)
		})
	}
}

func TestDefaultUserAgentHasNoDevelSentinel(t *testing.T) {
	assert.False(t, strings.Contains(DefaultUserAgent, "(devel)"), DefaultUserAgent)
}
