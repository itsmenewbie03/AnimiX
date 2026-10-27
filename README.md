# AnimiX 📺 [![author/maintainer](https://img.shields.io/badge/by-itsmenewbie03-7C3AED.svg?logo=github&labelColor=181717&longCache=true&style=flat-square)](https://github.com/itsmenewbie03)

**AnimiX** is an experimental terminal frontend for anime and manga that speaks
the AnymeX desktop runtime and Aniyomi extensions. It is independent and not
affiliated with or endorsed by the AnymeX project.

## 🧰 Requirements

You'll need these installed before your first run:

- [Java 21](https://adoptium.net/) or a compatible runtime
- [`mpv`](https://mpv.io/installation/) for video playback
- [Go 1.24+](https://go.dev/dl/) when running or building from source
- AnymeX-compatible extension JARs — install them from inside AnimiX, or point
  it at an existing Aniyomi extension folder

AnimiX drives the [AnymeX](https://github.com/RyanYuuki/AnymeX) desktop runtime,
so the same extension ecosystem applies.

## 🛠️ Setup

From the project root, install the pinned runtime first:

```sh
go run . --install-runtime
```

The runtime download is SHA-256 verified. After that, launch AnimiX with:

```sh
go run .
```

If your extensions live somewhere else:

```sh
go run . --extensions /path/to/AnymeX/Extensions/Aniyomi
```

Testing unreleased runtime bridge changes? Pass your rebuilt JAR explicitly —
`--install-runtime` installs the pinned release and won't test your local build:

```sh
go run . --runtime ../AnymeXExtensionRuntimeBridge/RuntimeBridges/Desktop/build/libs/desktop_bridge.jar
```

Need to check Java, the bridge process, and the extension directory without
opening the TUI?

```sh
go run . --diagnose
```

## 🔨 Building

The build produces a single `animix` binary:

```sh
go build -o animix .
```

## 🚀 Running

```sh
./animix
```

Use the arrow keys or `j`/`k` to move, `Enter` to select, `Esc` to go back, and
`q` to quit. Handy shortcuts:

| Key | What it does |
| --- | --- |
| `h` / `l`, `Tab`, `←` / `→`, `1` / `2` | Switch between Anime and Manga |
| `w` | Resume your most recent local playback |
| `d` or `/` | Open Discover and search with an installed source |
| `t` | Connect or manage AniList tracking |
| `r` | Manage extension repositories |
| `space` | Toggle **Remember server** on the stream screen |
| `a` / `s` | Search an alternative title / change source |
| `Ctrl+O` | Open the source domain or AniList authorization page |
| `c` | Start Cloudflare recovery manually |
| `x` | Sign out of AniList from the library screen |

## 🏠 Home, Episodes & History

The Anime tab mirrors AnymeX's flow: recent **New Episodes** for shows with
unwatched released episodes, plus a separate **Continue Watching** list of
AniList entries with `CURRENT` status. Finished shows drop off New Episodes
after seven days.

- Selecting a New Episodes card searches with the remembered anime source,
  opens the closest match automatically, and highlights the next unwatched
  episode.
- Episode rows use a two-line selector: tracker-watched titles are dimmed but
  still selectable, `󰈈` marks local completion, and provider episode titles
  appear on the second line when available.
- Source and title mappings are remembered per AniList media ID.
- Local mpv history stays separate from AniList. **Resume Watching** shows the
  newest incomplete playback first, records final position and duration when
  mpv exits, and hides episodes at least 99% complete.

## 📺 Playback & Streams

Selecting an anime stream launches `mpv` with the required HTTP headers and
subtitles, then returns you to the episode selector when playback ends.

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

## 🛡️ Cloudflare Recovery

When the runtime reports a Cloudflare 403/429/503 challenge, AnimiX cancels the
correlated request and opens a recovery prompt.

1. Press `Ctrl+O` to open the source domain and solve the challenge.
2. Paste the browser's Cookie request header and its exact User-Agent.
3. The runtime receives both values and retries the interrupted request.

Press `c` on a source-backed screen to start recovery manually when an
extension hides the underlying HTTP error. Credentials are stored per source in
a user-only (`0600`) configuration file. A 10-second request timeout remains as
a fallback for old runtimes and unrecognized challenges.

## 🧩 Extensions & Repositories

From the sources screen, press `r` to manage extension repositories. Use `a` to
add an anime or manga repository, `Enter` to browse it, and `d` to remove it.
Selecting an available extension downloads its APK and asks the runtime bridge
to convert it into a desktop JAR.

While browsing extensions, press `/` to search by name or package, `l` to cycle
the language filter, and `c` to clear both filters. Installed entries are marked
`[installed]`; select one and press `d` to unload it and remove its JAR.

Installed sources are not shown on Home. Press `d` (or `/`) to open Discover,
choose an installed source, and search with it. Continue Watching and New
Episodes remain separate selectable lists on the Anime home tab.

## 🎯 AniList Tracking

Press `t` from Home to connect AniList. On the token screen, `Ctrl+O` opens the
bundled AniList authorization page; paste the returned token to finish. The
tracking screen combines anime and manga entries — select one to change its
progress or status, then save it back to [AniList](https://anilist.co). Tokens
are stored in the CLI configuration file with user-only permissions. Press `x`
from the library to sign out.

## 🧪 Tests

Run the suite and the vet checks from the project root:

```sh
go test ./...
go vet ./...
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
- Manga page URL resolution
- AniList anime/manga library and tracking updates

MAL/Simkl tracking, CloudStream repository management, media downloads, and an
integrated manga reader are not implemented yet.

## 🐛 Issues & Ideas

Found a bug or have an idea? Reach out on
[GitHub](https://github.com/itsmenewbie03) — I'm always happy to hear it.

## 🫶 Contributing

Pull requests are welcome. I'll review them personally, so don't be shy about
opening one. Thanks for checking out AnimiX — happy watching! 🍿
