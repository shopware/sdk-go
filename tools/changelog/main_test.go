package main

import "testing"

func TestParseRequiredVersion(t *testing.T) {
	const path = "github.com/shopware/sdk-go"
	tests := []struct {
		name    string
		content string
		want    SemVer
		found   bool
	}{
		{
			name: "block form",
			content: "module m\n\ngo 1.25.0\n\nrequire (\n" +
				"\tgithub.com/shopware/sdk-go v1.2.3\n" +
				"\tgithub.com/other/thing v0.1.0\n)\n",
			want: SemVer{1, 2, 3}, found: true,
		},
		{
			name:    "block form with indirect comment",
			content: "require (\n\tgithub.com/shopware/sdk-go v1.0.0 // indirect\n)\n",
			want:    SemVer{1, 0, 0}, found: true,
		},
		{
			name:    "single-line form",
			content: "module m\n\nrequire github.com/shopware/sdk-go v2.0.1\n",
			want:    SemVer{2, 0, 1}, found: true,
		},
		{
			name:    "not required",
			content: "module m\n\nrequire github.com/other/thing v1.0.0\n",
			found:   false,
		},
	}
	for _, tt := range tests {
		got, found, err := parseRequiredVersion([]byte(tt.content), path)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tt.name, err)
			continue
		}
		if found != tt.found {
			t.Errorf("%s: found=%v want %v", tt.name, found, tt.found)
			continue
		}
		if found && got != tt.want {
			t.Errorf("%s: got %v want %v", tt.name, got, tt.want)
		}
	}
}
