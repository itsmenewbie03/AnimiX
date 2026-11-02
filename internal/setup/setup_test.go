package setup

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/itsmenewbie03/animix/internal/anilist"
	"github.com/itsmenewbie03/animix/internal/catalog"
	appconfig "github.com/itsmenewbie03/animix/internal/config"
	"github.com/itsmenewbie03/animix/internal/repository"
)

type fakeRepositories struct {
	items []repository.Extension
	err   error
}

func (f fakeRepositories) Fetch(url, itemType string) ([]repository.Extension, error) {
	if url != RepositoryURL || itemType != "anime" {
		return nil, errors.New("unexpected repository request")
	}
	return f.items, f.err
}

type fakeInstaller struct {
	installed *repository.Extension
}

func (f *fakeInstaller) Install(_ context.Context, item repository.Extension, _ string) (string, error) {
	f.installed = &item
	return "/extensions/anikoto.jar", nil
}

type fakeCatalog struct{ sources []catalog.Source }

func (f fakeCatalog) Sources(context.Context, string) ([]catalog.Source, error) {
	return f.sources, nil
}

type fakeStore struct {
	config appconfig.Config
	added  []appconfig.Repository
	saves  int
}

func (f *fakeStore) Load() (appconfig.Config, error) { return f.config, nil }
func (f *fakeStore) Save(config appconfig.Config) error {
	f.config = config
	f.saves++
	return nil
}
func (f *fakeStore) Add(repo appconfig.Repository) error {
	for _, existing := range f.config.Repositories {
		if existing == repo {
			return nil
		}
	}
	f.config.Repositories = append(f.config.Repositories, repo)
	f.added = append(f.added, repo)
	return nil
}

type fakeAniList struct {
	wantToken string
	viewer    anilist.Viewer
	err       error
}

func (f fakeAniList) Viewer(_ context.Context, token string) (anilist.Viewer, error) {
	if token != f.wantToken {
		return anilist.Viewer{}, errors.New("unexpected token")
	}
	return f.viewer, f.err
}

type fakePrompt struct {
	interactive bool
	confirms    []bool
	secrets     []string
	opened      []string
}

func (f *fakePrompt) Interactive() bool { return f.interactive }
func (f *fakePrompt) Confirm(string) (bool, error) {
	value := f.confirms[0]
	f.confirms = f.confirms[1:]
	return value, nil
}
func (f *fakePrompt) Secret(string) (string, error) {
	value := f.secrets[0]
	f.secrets = f.secrets[1:]
	return value, nil
}
func (f *fakePrompt) Open(url string) error {
	f.opened = append(f.opened, url)
	return nil
}

func recommendedExtension() repository.Extension {
	return repository.Extension{
		Name: "Anikoto", Package: AnikotoPackage, Version: "16.10", Type: "anime",
		SourceIDs: []string{"123"}, APKURL: "https://example.test/anikoto.apk",
	}
}

func TestRunInstallsExactPackageAndSkipsAccountsWhenNonInteractive(t *testing.T) {
	store := &fakeStore{config: appconfig.Config{Repositories: []appconfig.Repository{{URL: "https://custom.test", Type: "anime"}}}}
	installer := &fakeInstaller{}
	output := new(bytes.Buffer)
	service := Service{
		Repositories: fakeRepositories{items: []repository.Extension{
			{Name: "Anikoto copy", Package: "invalid.similar.package"}, recommendedExtension(),
		}},
		Installer: installer,
		Catalog:   fakeCatalog{sources: []catalog.Source{{ID: "123", Type: "anime"}}},
		Store:     store, Prompt: &fakePrompt{}, Output: output,
	}
	if err := service.Run(context.Background(), "/extensions"); err != nil {
		t.Fatal(err)
	}
	if installer.installed == nil || installer.installed.Package != AnikotoPackage {
		t.Fatalf("installed %#v", installer.installed)
	}
	wantRepos := []appconfig.Repository{{URL: "https://custom.test", Type: "anime"}, {URL: RepositoryURL, Type: "anime"}}
	if !reflect.DeepEqual(store.config.Repositories, wantRepos) {
		t.Fatalf("repositories = %#v, want %#v", store.config.Repositories, wantRepos)
	}
	if !strings.Contains(output.String(), "non-interactive") {
		t.Fatalf("output did not explain account skip: %q", output.String())
	}
}

func TestRunLinksAniListAndOffersOfficialDiscordIPC(t *testing.T) {
	store := &fakeStore{}
	prompt := &fakePrompt{interactive: true, confirms: []bool{true, true}, secrets: []string{"anilist-secret"}}
	service := Service{
		Repositories: fakeRepositories{items: []repository.Extension{recommendedExtension()}},
		Installer:    &fakeInstaller{}, Catalog: fakeCatalog{sources: []catalog.Source{{Package: AnikotoPackage, Type: "anime"}}},
		AniList: fakeAniList{wantToken: "anilist-secret", viewer: anilist.Viewer{ID: 1, Name: "viewer"}},
		Store:   store, Prompt: prompt,
	}
	if err := service.Run(context.Background(), "/extensions"); err != nil {
		t.Fatal(err)
	}
	if store.config.AniListToken != "anilist-secret" || store.config.DiscordToken != "" {
		t.Fatalf("account setup saved unexpected credentials: %#v", store.config)
	}
	if store.config.Discord == nil || !store.config.Discord.Enabled || store.config.Discord.Transport != "ipc" {
		t.Fatalf("official Discord IPC was not enabled: %#v", store.config.Discord)
	}
	if !store.config.Discord.ArtworkEnabled() {
		t.Fatal("setup did not keep poster artwork on by default")
	}
	if store.saves != 2 {
		t.Fatalf("account steps saved %d times, want 2 independently resumable saves", store.saves)
	}
	if !reflect.DeepEqual(prompt.opened, []string{anilist.AuthorizeURL}) {
		t.Fatalf("opened = %#v", prompt.opened)
	}
}

func TestRunPreservesExistingCredentialsAndPreferences(t *testing.T) {
	store := &fakeStore{config: appconfig.Config{
		AniListToken: "existing-anilist", DiscordToken: "existing-discord",
		Discord: &appconfig.DiscordSettings{Enabled: true, ShowBrowsing: true},
	}}
	prompt := &fakePrompt{interactive: true}
	service := Service{
		Repositories: fakeRepositories{items: []repository.Extension{recommendedExtension()}},
		Installer:    &fakeInstaller{}, Catalog: fakeCatalog{sources: []catalog.Source{{Package: AnikotoPackage, Type: "anime"}}},
		Store: store, Prompt: prompt,
	}
	if err := service.Run(context.Background(), "/extensions"); err != nil {
		t.Fatal(err)
	}
	if store.config.AniListToken != "existing-anilist" || store.config.DiscordToken != "existing-discord" || !store.config.Discord.Enabled {
		t.Fatalf("existing account state changed: %#v", store.config)
	}
	if store.saves != 0 {
		t.Fatalf("account config saved %d times despite no changes", store.saves)
	}
}

func TestRunDoesNotSaveInvalidAniListToken(t *testing.T) {
	store := &fakeStore{}
	service := Service{
		Repositories: fakeRepositories{items: []repository.Extension{recommendedExtension()}},
		Installer:    &fakeInstaller{}, Catalog: fakeCatalog{sources: []catalog.Source{{Package: AnikotoPackage, Type: "anime"}}},
		AniList: fakeAniList{wantToken: "bad", err: errors.New("unauthorized")}, Store: store,
		Prompt: &fakePrompt{interactive: true, confirms: []bool{true}, secrets: []string{"bad"}},
	}
	err := service.Run(context.Background(), "/extensions")
	if err == nil || !strings.Contains(err.Error(), "validate AniList token") {
		t.Fatalf("error = %v", err)
	}
	if store.config.AniListToken != "" || store.saves != 0 {
		t.Fatal("invalid AniList token was saved")
	}
}

func TestParseJavaMajor(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		output string
		want   int
	}{
		{`openjdk version "21.0.10" 2026-01-20`, 21},
		{`java version "17.0.12"`, 17},
		{`java version "1.8.0_402"`, 8},
	} {
		got, err := ParseJavaMajor(test.output)
		if err != nil || got != test.want {
			t.Fatalf("ParseJavaMajor(%q) = %d, %v; want %d", test.output, got, err, test.want)
		}
	}
}
