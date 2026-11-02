package setup

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/itsmenewbie03/animix/internal/anilist"
	"github.com/itsmenewbie03/animix/internal/catalog"
	appconfig "github.com/itsmenewbie03/animix/internal/config"
	"github.com/itsmenewbie03/animix/internal/repository"
)

const (
	RepositoryURL  = "https://raw.githubusercontent.com/yuzono/anime-repo/repo/index.min.json"
	AnikotoPackage = "eu.kanade.tachiyomi.animeextension.en.anikoto"
)

type RepositoryClient interface {
	Fetch(repoURL, itemType string) ([]repository.Extension, error)
}

type ExtensionInstaller interface {
	Install(context.Context, repository.Extension, string) (string, error)
}

type Catalog interface {
	Sources(context.Context, string) ([]catalog.Source, error)
}

type AniListClient interface {
	Viewer(context.Context, string) (anilist.Viewer, error)
}

type Store interface {
	Load() (appconfig.Config, error)
	Save(appconfig.Config) error
	Add(appconfig.Repository) error
}

type Prompter interface {
	Interactive() bool
	Confirm(string) (bool, error)
	Secret(string) (string, error)
	Open(string) error
}

type Service struct {
	Repositories RepositoryClient
	Installer    ExtensionInstaller
	Catalog      Catalog
	AniList      AniListClient
	Store        Store
	Prompt       Prompter
	Output       io.Writer
}

func (s Service) Run(ctx context.Context, extensionDirectory string) error {
	if s.Repositories == nil || s.Installer == nil || s.Catalog == nil || s.Store == nil {
		return fmt.Errorf("setup service is incomplete")
	}
	out := s.Output
	if out == nil {
		out = io.Discard
	}

	fmt.Fprintln(out, "Fetching Yūzōnō anime repository…")
	extensions, err := s.Repositories.Fetch(RepositoryURL, "anime")
	if err != nil {
		return fmt.Errorf("fetch recommended repository: %w", err)
	}
	anikoto, ok := findExtension(extensions, AnikotoPackage)
	if !ok {
		return fmt.Errorf("recommended repository does not contain Anikoto package %s", AnikotoPackage)
	}
	if err := s.Store.Add(appconfig.Repository{URL: RepositoryURL, Type: "anime"}); err != nil {
		return fmt.Errorf("register recommended repository: %w", err)
	}
	fmt.Fprintf(out, "Installing Anikoto %s…\n", anikoto.Version)
	if _, err := s.Installer.Install(ctx, anikoto, extensionDirectory); err != nil {
		return fmt.Errorf("install Anikoto: %w", err)
	}

	fmt.Fprintln(out, "Validating Anikoto sources…")
	sources, err := s.Catalog.Sources(ctx, extensionDirectory)
	if err != nil {
		return fmt.Errorf("validate Anikoto sources: %w", err)
	}
	if !hasAnikotoSource(sources, anikoto) {
		return fmt.Errorf("Anikoto installed but exposed no usable anime source")
	}
	fmt.Fprintln(out, "Core recommended setup complete.")

	if s.Prompt == nil || !s.Prompt.Interactive() {
		fmt.Fprintln(out, "Skipping optional account linking in a non-interactive terminal.")
		return nil
	}
	return s.linkAccounts(ctx, out)
}

func (s Service) linkAccounts(ctx context.Context, out io.Writer) error {
	config, err := s.Store.Load()
	if err != nil {
		return fmt.Errorf("load account settings: %w", err)
	}
	if strings.TrimSpace(config.AniListToken) != "" {
		fmt.Fprintln(out, "AniList is already linked; keeping the existing token.")
	} else {
		link, err := s.Prompt.Confirm("Link an AniList account? [y/N]")
		if err != nil {
			return err
		}
		if link {
			if err := s.Prompt.Open(anilist.AuthorizeURL); err != nil {
				fmt.Fprintf(out, "Could not open a browser: %v\nOpen this URL manually: %s\n", err, anilist.AuthorizeURL)
			}
			token, err := s.Prompt.Secret("AniList token: ")
			if err != nil {
				return err
			}
			token = strings.TrimSpace(token)
			if token == "" {
				return fmt.Errorf("AniList token cannot be empty")
			}
			if s.AniList == nil {
				return fmt.Errorf("AniList validation is unavailable")
			}
			viewer, err := s.AniList.Viewer(ctx, token)
			if err != nil {
				return fmt.Errorf("validate AniList token: %w", err)
			}
			config.AniListToken = token
			if err := s.Store.Save(config); err != nil {
				return fmt.Errorf("save AniList account: %w", err)
			}
			fmt.Fprintf(out, "AniList linked as %s.\n", viewer.Name)
		}
	}

	preferences := config.DiscordPreferences()
	if preferences.Enabled || preferences.Transport != "" || strings.TrimSpace(config.DiscordToken) != "" {
		fmt.Fprintln(out, "Discord Rich Presence is already configured; keeping the existing preference.")
	} else {
		enable, err := s.Prompt.Confirm("Enable Discord Rich Presence through the desktop client? [y/N]")
		if err != nil {
			return err
		}
		preferences.Transport = "ipc"
		preferences.Enabled = enable
		preferences.AutoDisabled = false
		config.Discord = &preferences
		if err := s.Store.Save(config); err != nil {
			return fmt.Errorf("save Discord Rich Presence preference: %w", err)
		}
	}
	return nil
}

func findExtension(items []repository.Extension, packageName string) (repository.Extension, bool) {
	for _, item := range items {
		if item.Package == packageName {
			return item, true
		}
	}
	return repository.Extension{}, false
}

func hasAnikotoSource(sources []catalog.Source, extension repository.Extension) bool {
	ids := make(map[string]struct{}, len(extension.SourceIDs))
	for _, id := range extension.SourceIDs {
		ids[id] = struct{}{}
	}
	for _, source := range sources {
		if !source.IsAnime() {
			continue
		}
		if source.Package == extension.Package {
			return true
		}
		if _, ok := ids[source.ID]; ok {
			return true
		}
	}
	return false
}
