package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/itsmenewbie03/animix/internal/bridge"
	"github.com/itsmenewbie03/animix/internal/catalog"
	appconfig "github.com/itsmenewbie03/animix/internal/config"
	anyruntime "github.com/itsmenewbie03/animix/internal/runtime"
	"github.com/itsmenewbie03/animix/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	defaultRuntime, err := anyruntime.DefaultPath()
	if err != nil {
		fatal(err)
	}
	defaultExtensions := defaultExtensionDirectory()
	defaultConfig, err := appconfig.DefaultPath()
	if err != nil {
		fatal(err)
	}

	javaPath := flag.String("java", "java", "Java executable used for the runtime bridge")
	runtimePath := flag.String("runtime", defaultRuntime, "path to anymex_desktop_runtime.jar")
	extensionsPath := flag.String("extensions", defaultExtensions, "directory containing converted extension JARs")
	playerPath := flag.String("player", "mpv", "external video player executable")
	configPath := flag.String("config", defaultConfig, "path to CLI configuration")
	installRuntime := flag.Bool("install-runtime", false, "download and verify the pinned runtime bridge")
	diagnose := flag.Bool("diagnose", false, "verify the runtime and list installed sources without opening the TUI")
	flag.Parse()

	if *installRuntime {
		fmt.Fprintf(os.Stderr, "Installing AnymeX runtime %s…\n", anyruntime.Version)
		if err := anyruntime.Install(*runtimePath); err != nil {
			fatal(err)
		}
	}
	if _, err := os.Stat(*runtimePath); err != nil {
		fatal(fmt.Errorf("runtime not found at %s (run with --install-runtime or pass --runtime): %w", *runtimePath, err))
	}
	if _, err := exec.LookPath(*javaPath); err != nil {
		fatal(fmt.Errorf("Java executable %q not found: %w", *javaPath, err))
	}
	if err := os.MkdirAll(*extensionsPath, 0o755); err != nil {
		fatal(fmt.Errorf("create extensions directory: %w", err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := bridge.Start(ctx, *javaPath, *runtimePath)
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	service := catalog.NewService(client)

	if *diagnose {
		diagnosticContext, diagnosticCancel := context.WithTimeout(ctx, 2*time.Minute)
		defer diagnosticCancel()
		sources, err := service.Sources(diagnosticContext, *extensionsPath)
		if err != nil {
			fatal(fmt.Errorf("bridge diagnostic failed: %w", err))
		}
		fmt.Printf("Runtime: %s\nExtensions: %s\nSources loaded: %d\n", *runtimePath, *extensionsPath, len(sources))
		for _, source := range sources {
			fmt.Printf("- %s [%s, %s] %s\n", source.Name, source.Type, source.Language, source.ID)
		}
		return
	}

	program := tea.NewProgram(
		tui.New(service, appconfig.NewStore(*configPath), *extensionsPath, *playerPath),
		tea.WithAltScreen(),
	)
	if _, err := program.Run(); err != nil {
		fatal(err)
	}
}

func defaultExtensionDirectory() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("AnymeX", "Extensions", "Aniyomi")
	}
	return filepath.Join(home, "Documents", "AnymeX", "Extensions", "Aniyomi")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "AnimiX:", err)
	os.Exit(1)
}
