# Installing AnimiX

AnimiX uses the AnymeX desktop runtime and extensions from the broader
Tachiyomi-derived ecosystem. It does not host media or bundle third-party
content sources.

## Requirements

- A [Java Runtime Environment (JRE) 17+](https://adoptium.net/). A JDK also
  works, but is not required.
- [`mpv`](https://mpv.io/installation/) for video playback.
- `curl` and either `sha256sum` or `shasum` when using the install script.

[Go 1.24+](https://go.dev/dl/) is needed only when building from source.

## Automated recommended setup

The recommended setup is:

```sh
animix --setup
```

It performs an idempotent setup in this order:

1. Verify Java 17+ and `mpv` without installing system packages or requesting
   elevated privileges.
2. Install and select the latest Cloudflare-aware runtime from the
   [AnimiX runtime fork](https://github.com/itsmenewbie03/AnymeXExtensionRuntimeBridge).
3. Register Yūzōnō's third-party anime repository:
   `https://raw.githubusercontent.com/yuzono/anime-repo/repo/index.min.json`.
4. Locate and install Anikoto by its stable package identity,
   `eu.kanade.tachiyomi.animeextension.en.anikoto`.
5. Start the runtime and verify that the installed extension exposes a usable
   anime source.
6. Offer optional account linking, then launch AnimiX.

Existing repositories, extensions, credentials, history, mappings, and custom
runtimes must remain untouched. Re-running the command must repair or update the
recommended components without creating duplicates.

### Optional account linking

Core setup must complete before account prompts appear. Each prompt defaults to
**No**, and a non-interactive terminal skips all account linking.

- **AniList:** setup offers to open AnimiX's AniList authorization page,
  accept the returned token through masked input, validate it against the
  AniList viewer API, and save it only after validation succeeds.
- **Discord:** setup offers Discord Rich Presence through Discord's official
  local IPC interface, with **No** as the default. This requires the Discord
  desktop client and does not require or store a token.

IPC connection failures remain quiet while AnimiX retries. After ten continuous
minutes without reaching the Discord desktop client, AnimiX disables Rich
Presence and shows a 15-second warning. It shows one reminder on the next
startup, then consumes that reminder. Rich Presence remains disabled until it
is manually re-enabled from Discord settings.

Tokens must never be accepted as command-line flags, written to logs, or echoed
to the terminal.

### Headless Discord Gateway mode

Users who do not want to run the Discord desktop client may explicitly select
Gateway mode from AnimiX's Discord settings and enter a user-session token.
Gateway mode is never enabled by `animix --setup` and is not an automatic
fallback when IPC is unavailable.

#### Obtaining a Discord token

> **Warning:** a Discord user-session token grants access to your account. Never
> share it, paste it into a website, include it in a screenshot, or save it in a
> shell command. Discord does not provide an official user-token API, and
> automated user-account access may violate Discord's terms or result in account
> restrictions. Proceed only if you understand and accept that risk.

Using Discord in a desktop web browser:

1. Sign in at [discord.com/app](https://discord.com/app).
2. Open the browser's developer tools and select the **Network** tab.
3. Reload Discord or navigate between channels to create API requests.
4. Select a request sent to `discord.com/api/`.
5. In **Request Headers**, find the `Authorization` header and copy its value.
   Copy only the value, not the word `Authorization`.
6. Open AnimiX's Discord settings, press `t` to select Gateway mode, and paste
   it at the masked token prompt.

The value is a long, opaque string and may look like `ODx…<redacted>` or begin
with a different sequence. Its exact prefix and length can vary. Do not include
`Authorization:`, `Bearer`, surrounding quotes, or any whitespace. AnimiX does
not determine validity from the prefix.

Close developer tools afterward. If the token is ever exposed, treat the
account as compromised: change your Discord password, sign out other sessions,
and do not continue using the exposed token.

## Install the current release

Prebuilt binaries are published through GitHub Releases:

```sh
curl -fsSL https://raw.githubusercontent.com/itsmenewbie03/AnimiX/main/install.sh | bash
```

The script installs `animix` to `~/.local/bin`. Pin a release or choose another
destination when needed:

```sh
curl -fsSL https://raw.githubusercontent.com/itsmenewbie03/AnimiX/main/install.sh | bash -s -- --version v0.8.0
curl -fsSL https://raw.githubusercontent.com/itsmenewbie03/AnimiX/main/install.sh | bash -s -- --dir ~/bin
```

The same settings are available as `ANIMIX_VERSION`, `ANIMIX_INSTALL_DIR`, and
`ANIMIX_REPO` environment variables.

Install the recommended runtime before launching the current release:

```sh
animix --install-runtime fork@latest
```

Then launch `animix`, press `r` from Home, add the Yūzōnō repository URL shown
above as an **anime** repository, open it, search for **Anikoto**, and install
the extension.

## Advanced setup for existing ecosystem users

The ecosystem terminology is related but not identical:

- [Mihon](https://mihon.app/) is the direct spiritual continuation of
  Tachiyomi.
- [Aniyomi](https://aniyomi.org/) describes itself as a player and reader based
  on Mihon.
- [Anikku](https://github.com/komikku-app/anikku) describes itself as a player
  based on Aniyomi.
- [AnymeX](https://github.com/RyanYuuki/AnymeX) is an independent tracking
  client that supports compatible extension workflows; it is better described
  as ecosystem-compatible than as a direct Tachiyomi successor.

### Existing AnymeX desktop setup

AnimiX uses its own converted-extension directory by default:

```text
~/Documents/AnimiX/Extensions/Aniyomi
```

It does not automatically reuse or modify an AnymeX extension directory. Point
AnimiX at an existing compatible converted-JAR directory only when that sharing
is explicitly desired:

```sh
animix --extensions /path/to/converted/extensions
```

Use an existing compatible desktop runtime explicitly with:

```sh
animix --runtime /path/to/desktop_bridge.jar
```

### Existing Aniyomi or Anikku setup

Repository URLs and extension choices transfer conceptually, but Android APKs
and private Android app directories are not desktop extension JARs. Do not point
AnimiX directly at an Android installation. Add the same repository through
AnimiX and let the desktop runtime convert the selected APK into a compatible
JAR.

AnimiX never imports tracker tokens, cookies, history, or other credentials from
another application automatically.

## Runtime selection

The recommended runtime is the latest release from the AnimiX fork because it
emits Cloudflare challenge events used by the recovery flow:

```sh
animix --install-runtime fork@latest
```

Other supported choices are:

```sh
animix --install-runtime fork@v2.7.3-animix.1
animix --install-runtime upstream@latest
animix --install-runtime upstream@v2.7.3
```

Every published runtime download is SHA-256 verified. Installs are isolated by
source and version:

```text
~/.config/animix/runtime/<source>/<version>/anymex_desktop_runtime.jar
```

The latest install becomes the default and is recorded in
`~/.config/animix/runtime/current`. Use `--runtime` to override it.

### Slow sources and request timeouts

AnimiX gives each extension request 10 seconds by default. Some sources need
longer; Anikoto, for example, can take 5–7 seconds to return video links. Raise
the per-request guard in `~/.config/animix/config.toml`:

```toml
extension_timeout = 20
```

The value is in seconds and is clamped to 5–120. A timed-out request is retried
once automatically, then reported as a retryable failure. Timeouts never open
Cloudflare recovery, so this setting is safe to raise for slow sources.

## Build from source

```sh
go build -o animix .
./animix --install-runtime fork@latest
./animix
```

Testing an unpublished runtime bridge build:

```sh
./animix --runtime ../AnymeXExtensionRuntimeBridge/RuntimeBridges/Desktop/build/libs/desktop_bridge.jar
```

## Diagnose an installation

Verify Java, the bridge process, the selected runtime, and installed extensions
without opening the TUI:

```sh
animix --diagnose
```

## Uninstall and clean up

Removing only the executable keeps your settings, history, credentials,
runtimes, and converted extensions available for a later reinstall.

If the install script used its default destination, remove the executable with:

```sh
rm -f ~/.local/bin/animix
```

Use the directory passed through `--dir` or `ANIMIX_INSTALL_DIR` instead when a
custom installation destination was used. On Windows, remove `animix.exe` from
that destination.

### Remove AnimiX data

> **Warning:** the following cleanup is permanent. Back up anything you want to
> retain before deleting it.

AnimiX stores settings, managed state, credentials, history, and downloaded
runtimes in its configuration directory:

| Platform | Default configuration directory |
|---|---|
| Linux | `~/.config/animix`, or `$XDG_CONFIG_HOME/animix` when set |
| macOS | `~/Library/Application Support/animix` |
| Windows | `%APPDATA%\animix` |

Remove it on Linux with:

```sh
rm -rf "${XDG_CONFIG_HOME:-$HOME/.config}/animix"
```

Remove it on macOS with:

```sh
rm -rf "$HOME/Library/Application Support/animix"
```

Remove it from PowerShell on Windows with:

```powershell
Remove-Item -Recurse -Force "$env:APPDATA\animix"
```

If AnimiX was run with `--config`, remove that custom configuration file and
the adjacent `state.json` instead. Managed runtimes still use the platform
configuration directory shown above.

Probe builds may also have private diagnostic traces. Remove them with:

```sh
rm -rf "${XDG_STATE_HOME:-$HOME/.local/state}/animix"
```

On Windows, remove probe traces with:

```powershell
Remove-Item -Recurse -Force "$HOME\.local\state\animix"
```

### Remove converted extensions

The default converted-extension directory belongs to AnimiX:

```text
~/Documents/AnimiX/Extensions/Aniyomi
```

For a complete cleanup, remove the directory and every extension installed in
it:

```sh
rm -rf "$HOME/Documents/AnimiX/Extensions/Aniyomi"
```

If other extensions should be retained, remove only Anikoto installed by the
recommended setup:

```sh
rm -f "$HOME/Documents/AnimiX/Extensions/Aniyomi/eu.kanade.tachiyomi.animeextension.en.anikoto.jar"
```

PowerShell equivalent:

```powershell
Remove-Item -Recurse -Force "$HOME\Documents\AnimiX\Extensions\Aniyomi"
# Or remove only Anikoto:
Remove-Item -Force "$HOME\Documents\AnimiX\Extensions\Aniyomi\eu.kanade.tachiyomi.animeextension.en.anikoto.jar"
```

These commands apply to the default directory only. If `--extensions` was used
to select a shared or custom directory, remove only the files owned by AnimiX
that are no longer wanted.

Deleting local credentials does not revoke them with AniList or Discord. Revoke
AniList access from your AniList account when desired. If a Discord
user-session token was exposed rather than merely deleted, change your Discord
password and sign out other sessions.
