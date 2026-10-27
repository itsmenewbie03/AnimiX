package repository

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUninstallRemovesInstalledExtensionJar(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	extension := Extension{Name: "Example", Package: "org.example/unsafe"}
	path := filepath.Join(directory, "org.example_unsafe.jar")
	if err := os.WriteFile(path, []byte("jar"), 0o600); err != nil {
		t.Fatal(err)
	}
	installer := NewInstaller(nil)
	if !installer.IsInstalled(extension, directory) {
		t.Fatal("extension was not detected as installed")
	}
	removed, err := installer.Uninstall(extension, directory)
	if err != nil {
		t.Fatal(err)
	}
	if removed != path {
		t.Fatalf("removed path = %q, want %q", removed, path)
	}
	if installer.IsInstalled(extension, directory) {
		t.Fatal("extension still detected after uninstall")
	}
}

func TestUninstallRejectsExtensionWithoutSafeName(t *testing.T) {
	t.Parallel()
	installer := NewInstaller(nil)
	if _, err := installer.Uninstall(Extension{}, t.TempDir()); err == nil {
		t.Fatal("expected invalid extension error")
	}
}
