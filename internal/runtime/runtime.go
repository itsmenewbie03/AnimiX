package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const (
	Version  = "v2.7.3"
	Checksum = "87ea7e65eec806ae0890f78f3ad9a0f7e516ec84b43a772a83bb9d60b812a535"
	URL      = "https://github.com/RyanYuuki/AnymeXExtensionRuntimeBridge/releases/download/" + Version + "/anymex_desktop_runtime.jar"
)

func DefaultPath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "anymex-cli", "runtime", Version, "anymex_desktop_runtime.jar"), nil
}

func Install(destination string) error {
	if valid, _ := Verify(destination); valid {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	response, err := http.Get(URL) //nolint:gosec -- pinned URL and checksum
	if err != nil {
		return fmt.Errorf("download runtime: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download runtime: HTTP %s", response.Status)
	}

	temporary := destination + ".download"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(temporary)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return closeErr
	}
	valid, err := Verify(temporary)
	if err != nil || !valid {
		_ = os.Remove(temporary)
		return fmt.Errorf("runtime checksum mismatch")
	}
	return os.Rename(temporary, destination)
}

func Verify(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, err
	}
	return hex.EncodeToString(hash.Sum(nil)) == Checksum, nil
}
