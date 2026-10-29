package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/itsmenewbie03/animix/internal/bridge"
	"github.com/itsmenewbie03/animix/internal/catalog"
	appconfig "github.com/itsmenewbie03/animix/internal/config"
	anyruntime "github.com/itsmenewbie03/animix/internal/runtime"
	"github.com/itsmenewbie03/animix/internal/tui"
	appupdate "github.com/itsmenewbie03/animix/internal/update"
)

var version = "dev"

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
	var installRuntime installRuntimeFlag
	flag.Var(&installRuntime, "install-runtime", "install a runtime: bare for latest upstream, fork@latest, upstream@v2.7.3")
	diagnose := flag.Bool("diagnose", false, "verify the runtime and list installed sources without opening the TUI")
	editConfig := flag.Bool("e", false, "open the AnimiX configuration in $VISUAL or $EDITOR")
	os.Args = append([]string{os.Args[0]}, normalizeInstallArgs(os.Args[1:])...)
	flag.Parse()

	runtimeFlagSet := false
	flag.Visit(func(current *flag.Flag) {
		if current.Name == "runtime" {
			runtimeFlagSet = true
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := appconfig.NewStore(*configPath)
	if *editConfig {
		if err := store.Ensure(); err != nil {
			fatal(fmt.Errorf("prepare config: %w", err))
		}
		if err := openConfigEditor(*configPath); err != nil {
			fatal(err)
		}
		return
	}
	config, err := store.Load()
	if err != nil {
		fatal(fmt.Errorf("load config: %w", err))
	}

	if installRuntime.enabled {
		release, err := anyruntime.Resolve(ctx, installRuntime.spec)
		if err != nil {
			fatal(err)
		}
		destination := *runtimePath
		if !runtimeFlagSet {
			destination, err = anyruntime.Path(release.Source, release.Version)
			if err != nil {
				fatal(err)
			}
			*runtimePath = destination
		}
		fmt.Fprintf(os.Stderr, "Installing %s runtime %s…\n", release.Source, release.Version)
		if err := anyruntime.Install(ctx, release, destination); err != nil {
			fatal(err)
		}
		if !runtimeFlagSet {
			if err := anyruntime.SetCurrent(release.Source, release.Version); err != nil {
				fatal(fmt.Errorf("record default runtime: %w", err))
			}
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

	options := []tui.Option{
		tui.WithAutoPlayNextEpisode(config.AutoPlayNextEpisode),
		tui.WithResumeAniListSync(config.ResumeSyncMode()),
	}
	if config.UpdatesEnabled() {
		options = append(options, tui.WithUpdateCheck(appupdate.NewClient(), version))
	}
	program := tea.NewProgram(
		tui.New(service, store, *extensionsPath, *playerPath, options...),
		tea.WithAltScreen(),
	)
	if _, err := program.Run(); err != nil {
		fatal(err)
	}
}

func openConfigEditor(path string) error {
	editor := strings.TrimSpace(firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR")))
	var name string
	var args []string
	if editor != "" {
		fields := strings.Fields(editor)
		name, args = fields[0], fields[1:]
	} else if runtime.GOOS == "windows" {
		name = "notepad"
	} else {
		name = "vi"
	}
	command := exec.Command(name, append(args, path)...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("edit config with %s: %w", name, err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type installRuntimeFlag struct {
	enabled bool
	spec    string
}

func (install *installRuntimeFlag) String() string {
	return install.spec
}

func (install *installRuntimeFlag) IsBoolFlag() bool {
	return true
}

func (install *installRuntimeFlag) Set(value string) error {
	switch value {
	case "", "true":
		install.enabled = true
		install.spec = ""
	case "false":
		install.enabled = false
		install.spec = ""
	default:
		install.enabled = true
		install.spec = value
	}
	return nil
}

func normalizeInstallArgs(args []string) []string {
	normalized := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if (argument == "--install-runtime" || argument == "-install-runtime") &&
			index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
			normalized = append(normalized, argument+"="+args[index+1])
			index++
			continue
		}
		normalized = append(normalized, argument)
	}
	return normalized
}

func defaultExtensionDirectory() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("AnimiX", "Extensions", "Aniyomi")
	}
	return filepath.Join(home, "Documents", "AnimiX", "Extensions", "Aniyomi")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "AnimiX:", err)
	os.Exit(1)
}
