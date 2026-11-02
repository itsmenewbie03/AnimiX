# AnimiX 📺 [![author/maintainer](https://img.shields.io/badge/by-itsmenewbie03-7C3AED.svg?logo=github&labelColor=181717&longCache=true&style=flat-square)](https://github.com/itsmenewbie03)

**AnimiX** is an experimental terminal frontend for anime and manga that speaks
the AnymeX desktop runtime and Aniyomi extensions. It is independent and not
affiliated with or endorsed by the AnymeX project.

## 📦 Install

See **[INSTALL.md](INSTALL.md)** for requirements, current installation steps,
the recommended `animix --setup` fast path, and advanced setup for existing
AnymeX, Aniyomi, Anikku, or other Tachiyomi-derived ecosystem users.

## ⚙️ Configuration

AnimiX stores user-editable settings in `~/.config/animix/config.toml` and
keeps app-managed history, credentials, mappings, and source choices in
`state.json`. Existing combined TOML or legacy `config.json` files are split
automatically and retained as `.bak` files. Open only the settings file in
`$VISUAL`, `$EDITOR`, or the platform fallback editor with:

```sh
animix -e
```

Release builds check GitHub for updates in the background. A small notification
and footer shortcut appear when a newer release is available; press `u` to
start downloading immediately. The update screen reports measured download
progress, verifies the published checksum, and atomically replaces the binary.
Disable the check in TOML when desired:

```toml
check_updates = false
```

Next-episode playback is tri-state. Leave the setting out to ask after each
completed episode, set it to `true` to always continue, or `false` to never
prompt:

```toml
auto_play_next_episode = true
```

Resume entries linked to AniList can follow tracker progress across devices.
The default `filter` mode derives Resume visibility from current AniList
progress without changing local playback history:

```toml
sync_resume_with_anilist = "filter"
```

Use `"off"` to ignore AniList progress. Use `"mark_watched"` to persist a
remotely-watched marker in local history, keeping matched episodes out of Resume
across relaunches and AniList progress rollbacks without deleting their saved
playback position.

### Extension request timeout

`extension_timeout` sets the per-request guard for search, details, video, and
page calls, in seconds. The default is `10`; raise it for slow sources such as
Anikoto, which can take 5–7 seconds to return video links. Values are clamped to
5–120 seconds:

```toml
extension_timeout = 20
```

A request that reaches this limit is retried once automatically. If the retry
also times out, the request is reported as a retryable failure. Timeouts are not
treated as Cloudflare challenges, so increasing the timeout never opens the
recovery prompt by itself.

## 🔨 Building

The build produces a single `animix` binary:

```sh
go build -o animix .
```

CI runs tests, `go vet`, and a build on every push and pull request. Pushing a
`v*` tag triggers the release workflow, which publishes binaries named
`animix_<os>_<arch>[.exe]` plus `SHA256SUMS` for the install script.

## 🚀 Running

```sh
./animix
```

Use the arrow keys or `j`/`k` to move, `gg`/`G` to jump to the first/last item,
`Enter` to select, `Esc` to go back, and `q` to quit. Press `?` anywhere outside
a text field for the complete categorized shortcut reference.

| Key                                    | What it does                                         |
| -------------------------------------- | ---------------------------------------------------- |
| `1` / `2`                              | Switch between Anime and Manga                       |
| `h` / `l`, `Tab`, `←` / `→`            | Switch Home feeds                                    |
| `Tab` / `Shift+Tab`, `h` / `l`, `←` / `→` | Switch scanlator in the Manga chapter list          |
| `/` / `c`                              | Filter the active Home feed / clear its filter       |
| `d`                                    | Open Discover and search with an installed source    |
| `t`                                    | Connect or manage AniList tracking                   |
| `p`                                    | Manage Discord presence from Home                   |
| `r`                                    | Manage extension repositories                        |
| `space`                                | Toggle **Remember server** on the stream screen      |
| `a` / `s`                              | Search an alternative title / change source          |
| `Ctrl+O`                               | Reopen reader / open source or AniList authorization |
| `Ctrl+E`                               | Switch extension from Cloudflare recovery            |
| `c`                                    | Start Cloudflare recovery manually                   |
| `x`                                    | Sign out of AniList from the library screen          |

## 🏠 Home, Episodes & History

The Anime Home uses full-height tabs for **Resume**, **New Episodes**, and
**Watching**. Resume keeps incomplete local playback ordered by recent activity.
New Episodes mirrors AnymeX's release rules and sorts recent unwatched releases
by release date. Watching preserves AniList's recently-updated order for entries
with `CURRENT` status. A title may intentionally appear in both New Episodes and
Watching, matching AnymeX. Finished shows drop off New Episodes after seven
days.

The Manga Home uses **Resume**, **Reading**, **Completed**, **Paused**,
**Dropped**, **Planning**, **Rereading**, and **All** feeds. Tracker-status feeds
come from AniList and empty status feeds stay hidden. Resume is local reading
history: it records the exact chapter and visible page and reopens there after a
restart. AniList does not provide reliable chapter-release data, so Manga Home
does not fabricate a New Chapters feed.

Manga chapter lists show **All** plus the source's scanlator groups, such as
**official** and **unofficial** on MangaFire. Use `Tab` / `Shift+Tab`, `h` / `l`,
or the left/right arrows to switch groups. Rows label the version, and **All**
keeps every release available. The filter starts at All for each new title or
source and stays selected when returning from the reader. Resume retains the
chosen chapter version and page.

Selecting a chapter opens an ad-free reader in the system browser. AnimiX serves
the reader only on `127.0.0.1`, proxies the extension's required image headers,
and renders the source's direct full-resolution pages in a continuous fit-width
layout. The title and page controls hide while scrolling down and return when
scrolling up or moving the pointer to the top. Arrow keys, `j`/`k`, Space, Page
Up/Down, Home, and End scroll the reader. Image loading preserves the current
page's position as its layout settles. Press `Ctrl+O` on the terminal's Browser
Reader screen to reopen the tab at the latest saved page. Returning to the
chapter list closes the browser-reader session and its loopback listener.

- Selecting a New Episodes card searches with the remembered anime source,
  opens the closest match automatically, and highlights the next unwatched
  episode.
- Episode rows use a two-line selector: tracker-watched titles are dimmed but
  still selectable, `󰈈` marks local completion, and provider episode titles
  appear on the second line when available.
- Source and title mappings are remembered per AniList media ID.
- Tracked Anime and Manga resolution tries the saved mapping, AniList preferred,
  English, Romaji, native, and synonym titles, then normalized variants before
  using the source's best available fallback. An exact English, Romaji, native,
  preferred, or synonym match always wins over a saved mapping, so a stale
  mapping is repaired automatically. Low-confidence fallbacks are never saved as
  mappings.
- Local mpv history stays separate from AniList. **Resume Watching** shows the
  newest incomplete playback first, records final position and duration when
  mpv exits, and hides episodes at least 90% complete. AniList-linked entries
  also leave Resume when tracker progress reaches that episode. Fractional
  episode numbers use the next whole progress value (for example, `5.5` is
  marked watched at AniList progress `6`).

## 📺 Playback & Streams

Selecting an anime stream launches `mpv` with the required HTTP headers and
subtitles, then returns you to the episode selector when playback ends.

- After a cleanly completed episode, AnimiX can resolve and play the next
  available episode. With `auto_play_next_episode` unset, the prompt offers
  **No**, **Yes**, **Always**, and **Never**; the last two persist `true` or
  `false` in `config.toml`. User quits, playback errors, and early stream
  failures never advance automatically.
- For AniList-linked titles, the stream screen has a **Remember server**
  checkbox. When enabled, the selected quality is saved for that media ID,
  marked `Default`, and auto-played on later episodes. A single available stream
  auto-plays too. Press `space` to toggle it; unchecking clears the preference.
- AnimiX queries [AniSkip](https://aniskip.com) with the series' MAL ID — the
  same source AnymeX uses — and writes opening, ending, and recap ranges as mpv
  chapters. The marks appear on mpv's seek bar, and chapter navigation jumps to
  each range's start and end. Lookup failures never block playback.
- Playback launched from an AniList-linked Home entry updates AniList after 90%
  of the episode has played. Progress never moves backward, finishing the final
  known episode marks the anime `COMPLETED`, and `REPEATING` is preserved.
- Resuming from **Resume Watching** resolves the episode again before passing
  the saved position to mpv. This replaces expired `localhost` proxy URLs left
  by an earlier runtime process.
- After repeated local-proxy HTTP 500 responses, mpv exits before ffmpeg can
  skip the rest of the playlist. AnimiX saves the current position and opens
  Cloudflare recovery. Applying new browser credentials resolves a fresh stream
  URL and restarts mpv from that position. A clean EOF before 90% follows the
  same recovery path instead of being marked finished.
- HLS playback uses a fresh HTTP connection per segment; this avoids stale
  keep-alive sockets from extension-local proxies that close idle connections
  after five seconds. The demuxer buffer is capped at 32 MiB with two minutes of
  readahead, matching AnymeX Android and preventing AnimePahe's CDN from being
  flooded by hundreds of segment requests at startup.
- To capture an mpv log for a stream that still ends early, set
  `ANIMIX_MPV_LOG=/tmp/animix-mpv.log` before launching AnimiX.

For timestamp and pause/reconnect diagnosis, build the opt-in playback probe:

```sh
go build -tags probe -o animix-probe .
./animix-probe
```

Each mpv session writes a private JSONL trace under
`$XDG_STATE_HOME/animix/probes` (or `~/.local/state/animix/probes`). Set
`ANIMIX_PROBE_DIR` to choose another directory. Probe traces contain sampled
mpv IPC state and redacted stderr; command arguments, stream URLs, cookies, and
request headers are not recorded. Review traces before sharing them.

## 🛡️ Cloudflare Recovery

When the runtime reports a Cloudflare 403/429/503 challenge, AnimiX cancels the
correlated request and opens a recovery prompt.

1. Press `Ctrl+O` to open the source domain and solve the challenge.
2. Paste the browser's Cookie request header and its exact User-Agent.
3. The runtime receives both values and retries the interrupted request.

Press `c` on a source-backed screen to start recovery manually when an
extension hides the underlying HTTP error. Credentials are stored per source in
a user-only (`0600`) configuration file.

Press `Ctrl+E` in the recovery prompt to switch to another installed extension
and search again instead of obtaining new credentials. Slow sources are not
assumed to be Cloudflare-protected: a request that reaches the configured
timeout is retried once, then reported as a retryable failure and never opens
recovery on its own.

### ⚠️ Use a runtime that emits Cloudflare events

This recovery flow depends on unsolicited `cloudflare` events from the runtime.
**Stock upstream releases do not emit them yet.** Our fork's `main` branch does.

A bare `--install-runtime` installs the latest upstream release, so it will not
enable recovery. Install our fork explicitly:

```sh
go run . --install-runtime fork@latest
```

Here **fork** means our fork at
[itsmenewbie03/AnymeXExtensionRuntimeBridge](https://github.com/itsmenewbie03/AnymeXExtensionRuntimeBridge).
Selecting `upstream@latest` or another stock upstream tag is still available,
but those builds leave you on the timeout fallback (10 seconds by default)
because they do not emit `cloudflare` events. If you use `--runtime` directly, point it at a build or
release from our fork, not a stock upstream JAR.

**Build the fork locally:**

```sh
git clone https://github.com/itsmenewbie03/AnymeXExtensionRuntimeBridge.git
cd AnymeXExtensionRuntimeBridge/RuntimeBridges/Desktop
bash ./gradlew shadowJar
```

Then launch AnimiX with the built JAR:

```sh
go run . --runtime \
  /path/to/AnymeXExtensionRuntimeBridge/RuntimeBridges/Desktop/build/libs/desktop_bridge.jar
```

**Fork releases** are built by the
[Desktop runtime workflow](https://github.com/itsmenewbie03/AnymeXExtensionRuntimeBridge/actions/workflows/desktop-runtime.yml)
on every `v*` tag and published to
[Releases](https://github.com/itsmenewbie03/AnymeXExtensionRuntimeBridge/releases)
as `anymex_desktop_runtime.jar` plus a `.sha256` checksum. To publish one:

```sh
git tag v2.7.3-animix.2
git push origin v2.7.3-animix.2
```

The same workflow also supports manual dispatch, which uploads an unsigned
workflow artifact instead of a release. Manual runs only appear in the Actions
tab once the workflow file exists on the repository's default branch.

To use a downloaded release directly:

```sh
go run . --runtime ~/Downloads/anymex_desktop_runtime.jar
```

## 🧩 Extensions & Repositories

From the sources screen, press `r` to manage extension repositories. Use `a` to
add an anime or manga repository, `Enter` to browse it, and `d` to remove it.
Selecting an available extension downloads its APK and asks the runtime bridge
to convert it into a desktop JAR.

While browsing extensions, press `/` to search by name or package, `l` to cycle
the language filter, and `c` to clear both filters. Installed entries are marked
`[installed]`; select one and press `d` to unload it and remove its JAR.

Installed sources are not shown on Home. Press `d` to open Discover, choose an
installed source, and search with it. Resume, New Episodes, and Watching remain
separate filterable tabs on Anime Home.

## 🎯 AniList Tracking

Press `t` from Home to connect AniList. On the token screen, `Ctrl+O` opens the
bundled AniList authorization page; paste the returned token to finish. The
tracking screen combines anime and manga entries — select one to change its
progress or status, then save it back to [AniList](https://anilist.co). Tokens
are stored in the CLI configuration file with user-only permissions. Press `x`
from the library to sign out.

## 💬 Discord Presence

Discord sharing is **off by default**. From Home, press `p`, then `e` to enable
sharing over the running Discord desktop client. IPC needs no token. To share
without the desktop client, press `t` and paste a Discord **user-session token**
into the masked input; press `Enter` to save it, then `e` to enable Gateway
sharing. Saving a token preserves the sharing preference.

AnimiX supports two Discord transports. Gateway mode connects directly from Go
on Linux, macOS, and Windows, so the desktop app can be closed. It is an
unofficial user-session integration: ordinary OAuth access tokens and bot tokens
cannot publish your personal activity through this connection. IPC mode uses
Discord's official local connection and requires the desktop client to be
running. Press `i` to use IPC or `g` to switch back to Gateway. Switching keeps
the saved token, so Gateway only prompts when no token exists.

On the Discord screen:

| Key | Action |
| --- | --- |
| `e` | Enable or disable sharing |
| `i` | Use the running Discord desktop client over local IPC |
| `g` | Switch to Gateway using the saved token |
| `t` | Set or replace the Gateway token |
| `b` | Toggle optional “Browsing AnimiX” activity |
| `a` | Toggle anime/manga poster artwork |
| `r` | Retry the connection and reload Discord settings |
| `x` | Forget the token and disable sharing |

Watching activity follows mpv's episode, position, speed, pause, and buffering
state while the TUI is suspended. Reading activity includes the chapter and
page; hidden tabs stop advertising reading, and a lost or closed tab expires
within a minute. Network interruptions reconnect automatically and replay the
latest activity. Updates are coalesced to respect Discord's send limits.

When poster artwork is enabled, watching uses the anime cover and reading uses
the manga cover as the activity's large image. Artwork is on by default, can be
disabled independently, and only applies while sharing is enabled. Linked
titles prefer AniList's CDN cover; unlinked titles use the extension poster.
Gateway mode converts public HTTPS covers through Discord's external-assets
endpoint. IPC mode passes the same public cover URL directly to the desktop
client. Missing or failed artwork falls back to `large_image`, then text.

Preferences live in `config.toml`. Sharing remains opt-in, while poster artwork
defaults on. AnimiX ships its public Discord application ID, so users do not
need to register an application:

```toml
[discord]
enabled = false
show_browsing = false
show_artwork = true
```

Linked titles include an AniList button, and clicking their poster opens the
same AniList page. Developers may override `application_id` and `large_image`
to test another Discord application and uploaded fallback asset. Poster artwork
defaults to on; set `show_artwork = false` to opt out. Missing or failed artwork
falls back to `large_image`, then text. After editing settings, press `r` or
restart AnimiX.

The token is stored unencrypted in managed `state.json`, alongside other account
credentials, with owner-only (`0600`) permissions on Unix. It is excluded from
the editable TOML and connection diagnostics. Presence uses title metadata;
stream URLs, playback headers, source cookies, and loopback reader URLs are not
included.

Connection status and “last activity sent” describe transport state, not a
rendering acknowledgment. The user-session protocol and optional artwork/button
formats can change; live Discord rendering has not yet been validated with an
independent account.

## 🧪 Tests

Run the suite and the vet checks from the project root:

```sh
go test ./...
go vet ./...
CGO_ENABLED=1 go test -race ./internal/discord ./internal/player ./internal/mangareader ./internal/tui
```

## 📌 Current Scope

- Runtime download and checksum verification
- Runtime process lifecycle and JSON request routing
- Installed Aniyomi extension loading
- Configurable anime and manga extension repositories
- JSON, protobuf, and gzip-compressed repository indexes
- Extension APK download and bridge-based desktop JAR conversion
- Search, details, and episodes/chapters
- Stream resolution and mpv playback
- Local Manga Resume with exact chapter/page restoration
- Ad-free system-browser manga reader with header-aware image proxying
- AniList anime/manga library and tracking updates
- Opt-in direct Discord Gateway presence for watching and reading

MAL/Simkl tracking, CloudStream repository management, media downloads, and an
embedded terminal manga reader are not implemented yet.

## 🐛 Issues & Ideas

Found a bug or have an idea? Reach out on
[GitHub](https://github.com/itsmenewbie03) — I'm always happy to hear it.

## 🫶 Contributing

Pull requests are welcome. I'll review them personally, so don't be shy about
opening one. Thanks for checking out AnimiX — happy watching! 🍿
