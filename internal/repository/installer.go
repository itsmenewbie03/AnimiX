package repository

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type Converter interface {
	ConvertAPK(ctx context.Context, apkPath, outputPath string) error
}

type Installer struct {
	converter Converter
	http      *http.Client
}

func NewInstaller(converter Converter) *Installer {
	return &Installer{
		converter: converter,
		http:      &http.Client{Timeout: 2 * time.Minute},
	}
}

func (i *Installer) Install(ctx context.Context, extension Extension, destinationDirectory string) (string, error) {
	if extension.APKURL == "" {
		return "", fmt.Errorf("extension %q has no APK URL", extension.Name)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, extension.APKURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "AnimiX")
	response, err := i.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("download extension: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download extension: HTTP %s", response.Status)
	}

	temporary, err := os.CreateTemp("", "animix-extension-*.apk")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	written, copyErr := io.Copy(temporary, io.LimitReader(response.Body, 256*1024*1024))
	closeErr := temporary.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if written < 4 {
		return "", fmt.Errorf("downloaded extension is empty")
	}
	file, err := os.Open(temporaryPath)
	if err != nil {
		return "", err
	}
	magic := make([]byte, 2)
	_, readErr := io.ReadFull(file, magic)
	_ = file.Close()
	if readErr != nil || string(magic) != "PK" {
		return "", fmt.Errorf("downloaded extension is not a valid APK/ZIP")
	}

	if err := os.MkdirAll(destinationDirectory, 0o755); err != nil {
		return "", err
	}
	outputPath, err := extensionPath(extension, destinationDirectory)
	if err != nil {
		return "", err
	}
	temporaryOutput := outputPath + ".installing.jar"
	_ = os.Remove(temporaryOutput)
	if err := i.converter.ConvertAPK(ctx, temporaryPath, temporaryOutput); err != nil {
		_ = os.Remove(temporaryOutput)
		return "", fmt.Errorf("convert extension: %w", err)
	}
	if _, err := os.Stat(temporaryOutput); err != nil {
		return "", fmt.Errorf("bridge did not create converted extension: %w", err)
	}
	if err := os.Remove(outputPath); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(temporaryOutput)
		return "", err
	}
	if err := os.Rename(temporaryOutput, outputPath); err != nil {
		_ = os.Remove(temporaryOutput)
		return "", err
	}
	return outputPath, nil
}

func (i *Installer) IsInstalled(extension Extension, destinationDirectory string) bool {
	path, err := extensionPath(extension, destinationDirectory)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (i *Installer) Uninstall(extension Extension, destinationDirectory string) (string, error) {
	path, err := extensionPath(extension, destinationDirectory)
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("extension %q is not installed", extension.Name)
		}
		return "", fmt.Errorf("remove extension: %w", err)
	}
	return path, nil
}

func extensionPath(extension Extension, destinationDirectory string) (string, error) {
	packageName := safeFilename(extension.Package)
	if packageName == "" {
		packageName = safeFilename(extension.Name)
	}
	if packageName == "" {
		return "", fmt.Errorf("extension has no valid package or name")
	}
	return filepath.Join(destinationDirectory, packageName+".jar"), nil
}

var unsafeFilename = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func safeFilename(value string) string {
	return unsafeFilename.ReplaceAllString(value, "_")
}
