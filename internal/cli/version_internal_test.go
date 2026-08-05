package cli

import "testing"

func TestResolveVersionFallsBackToModuleBuildInfo(t *testing.T) {
	tests := []struct {
		linker string
		module string
		want   string
	}{
		{linker: "v1.2.3", module: "v9.9.9", want: "v1.2.3"},
		{linker: "devel", module: "v1.2.3", want: "v1.2.3"},
		{linker: "devel", module: "(devel)", want: "devel"},
		{linker: "", module: "", want: "devel"},
	}
	for _, tt := range tests {
		if got := resolveVersion(tt.linker, tt.module); got != tt.want {
			t.Errorf("resolveVersion(%q, %q) = %q, want %q", tt.linker, tt.module, got, tt.want)
		}
	}
}
