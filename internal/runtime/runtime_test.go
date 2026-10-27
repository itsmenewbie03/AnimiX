package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyRejectsDifferentContent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "runtime.jar")
	if err := os.WriteFile(path, []byte("not the runtime"), 0o600); err != nil {
		t.Fatal(err)
	}
	valid, err := Verify(path, DefaultChecksum)
	if err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Fatal("Verify accepted an invalid runtime")
	}
}

func TestFallbackIsUpstreamRelease(t *testing.T) {
	t.Parallel()
	release := Fallback()
	if release.Source != SourceUpstream {
		t.Fatalf("fallback source = %q, want %q", release.Source, SourceUpstream)
	}
	if release.Version != DefaultVersion || release.Checksum != DefaultChecksum {
		t.Fatalf("fallback release = %#v", release)
	}
	wantURL := "https://github.com/RyanYuuki/AnymeXExtensionRuntimeBridge/releases/download/" + DefaultVersion + "/" + assetName
	if release.URL != wantURL {
		t.Fatalf("fallback URL = %q, want %q", release.URL, wantURL)
	}
}

func TestParseSpec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		spec    string
		source  string
		version string
		wantErr bool
	}{
		{spec: "", source: SourceUpstream, version: "latest"},
		{spec: "default", source: SourceUpstream, version: "latest"},
		{spec: "fork", source: SourceFork, version: "latest"},
		{spec: "FORK@latest", source: SourceFork, version: "latest"},
		{spec: "fork@", source: SourceFork, version: "latest"},
		{spec: "fork@v2.7.3-animix.1", source: SourceFork, version: "v2.7.3-animix.1"},
		{spec: "upstream@v2.7.3", source: SourceUpstream, version: "v2.7.3"},
		{spec: "latest", wantErr: true},
		{spec: "v2.7.3-animix.1", wantErr: true},
		{spec: "unknown@v1", wantErr: true},
		{spec: "fork@v1/2", wantErr: true},
		{spec: "fork@..", wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.spec, func(t *testing.T) {
			t.Parallel()
			source, version, err := parseSpec(test.spec)
			if test.wantErr {
				if err == nil {
					t.Fatalf("parseSpec(%q) succeeded, want error", test.spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSpec(%q): %v", test.spec, err)
			}
			if source != test.source || version != test.version {
				t.Fatalf("parseSpec(%q) = %q, %q, want %q, %q", test.spec, source, version, test.source, test.version)
			}
		})
	}
}

func TestResolveRejectsUnknownSource(t *testing.T) {
	t.Parallel()
	if _, err := Resolve(context.Background(), "unknown@latest"); err == nil {
		t.Fatal("Resolve accepted an unknown source")
	}
}

func TestPathUsesSourceAndVersion(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	path, err := Path(SourceUpstream, "v2.7.3")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(configHome, "anymex-cli", "runtime", SourceUpstream, "v2.7.3", assetName)
	if path != want {
		t.Fatalf("Path = %q, want %q", path, want)
	}
}

func TestDefaultPathFallsBackToUpstreamRelease(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	want, err := Path(SourceUpstream, DefaultVersion)
	if err != nil {
		t.Fatal(err)
	}
	if path != want {
		t.Fatalf("DefaultPath = %q, want %q", path, want)
	}
}

func TestSetCurrentChangesDefaultPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	source, version := SourceUpstream, "v9.9.9"
	installed, err := Path(source, version)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("runtime"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetCurrent(source, version); err != nil {
		t.Fatal(err)
	}
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != installed {
		t.Fatalf("DefaultPath = %q, want %q", path, installed)
	}
}

func TestDefaultPathIgnoresMissingCurrentRuntime(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SetCurrent(SourceFork, "v9.9.9"); err != nil {
		t.Fatal(err)
	}
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	want, err := Path(SourceUpstream, DefaultVersion)
	if err != nil {
		t.Fatal(err)
	}
	if path != want {
		t.Fatalf("DefaultPath = %q, want fallback %q", path, want)
	}
}
