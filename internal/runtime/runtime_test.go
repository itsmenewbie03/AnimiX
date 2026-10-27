package runtime

import (
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
	valid, err := Verify(path)
	if err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Fatal("Verify accepted an invalid runtime")
	}
}
