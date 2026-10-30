# Unified, opt-in Discord Gateway presence

Research and implementation proposal, 2026-10-30.

This records the pre-implementation audit. The implemented client lives in
`internal/discord`; see [Discord Presence in the README](../../README.md#-discord-presence)
for current setup and controls.

Audited revisions:

- AnymeX: `afbcc1b32ecf7110e31d305178a9a3f781e594ef`.
- AnimiX: `1ff4d1f495cc702ea21afc9e368b67eb0abcf0d3`.

## Direction

Use one direct Gateway client inside AnimiX on Linux, macOS, and Windows. A
background Go service owns the Discord WebSocket and retains the latest desired
activity. Users explicitly enable sharing and connect their account. This design
does not depend on a running Discord desktop app.

```text
mpv live observations ───┐
                        ├─> current activity ─> rate-limited publisher ─> Discord Gateway
reader session events ──┤                               ^
TUI navigation/settings ┘                         reconnect + replay
```

The main lesson from AnymeX is to make activity state independent of connection
state. Reconnecting must restore what the user is currently doing; it must not
invent a browsing activity or wait for the next playback event.

### Authentication constraint

AnymeX's Gateway path uses a **Discord user-session token**. Its embedded Discord
login page extracts that token from web-client storage, webpack exports, or
network activity, then passes it to `onLoginSuccess`. It is not an ordinary
application OAuth login. The token is used in Gateway Identify and authenticated
HTTP requests. [AX-login] [AX-rpc]

Public Discord OAuth documents `identify` as profile access, marks
`activities.write` unavailable to apps, and describes `rpc.activities.write` as
local-client RPC access for approved partners. A bot token represents the bot's
account, not the person using AnimiX; bot activities also have a reduced writable
field set. **A normal OAuth access token or a bot token cannot simply replace
AnymeX's user token for this design.** [D-oauth] [D-events]

Consequently, AnymeX-style human-account Gateway presence is an unofficial
user-session integration. The public Gateway documentation is useful for the
connection protocol, but does not promise support for every user-session payload
AnymeX sends. Opt-in controls AnimiX's behavior; it does not change that API
contract. Keep this distinction explicit in the connection UI and implementation.

## How AnymeX actually works

The live anchor is `DiscordRPCController` in
`AnymeX/lib/controllers/discord/discord_rpc.dart:103`. Its instance accessor uses
`Get.find` at line 139, and the production startup registers the concrete
controller with `Get.put(DiscordRPCController())` in `lib/main.dart:276`.
[AX-rpc] [AX-main]

The controller selects its transport using `Platform.isAndroid || Platform.isIOS`
at line 122. Mobile calls `_connectMobileGateway` at line 345; desktop calls
`_initializeDesktopRPC` at line 299, using `dart_discord_presence` and the local
Discord client. These are different paths, with different failure modes. AnimiX's
chosen reference is the **mobile Gateway implementation**, adapted to Go desktop.
[AX-rpc]

The production playback chain is:

1. `PlayerController.onInit`, `player_controller.dart:397`, calls `_updateRpc`.
2. `_updateRpc`, lines 526–538, passes media and episode state into
   `DiscordRPCController.instance.updateAnimePresence`.
3. `updateAnimePresence`, `discord_rpc.dart:538`, queues formatting and artwork
   work, then writes an opcode-3 Gateway payload at lines 610–649.
4. Duration changes and play/pause transitions also call these methods at
   `player_controller.dart:1284–1319`; seeking uses a ten-second trailing debounce
   at lines 2074–2087. [AX-player] [AX-rpc]

Manga chapter changes and page changes call `updateMangaPresence`; page updates
also wait for ten seconds without another page change. The controller publishes
browsing when Gateway `READY` arrives. Its enabled setting defaults to `true`,
although mobile still needs a saved token. [AX-reader] [AX-rpc]

## Reproduced controller-level failures

To distinguish executable behavior from code-reading guesses, the actual Dart
controller body was loaded unchanged into an isolated replay harness. Only its
platform, GetX/UI, storage, WebSocket, and HTTP collaborators were replaced with
synthetic fixtures. No Discord account or live Discord connection was used.

Temporary diagnostic tooling used Dart SDK 3.13.7 under `/tmp/opencode`; this adds
no Dart dependency to AnimiX. Harness files are under
`/tmp/opencode/anymex-rpc-replay/`. Controller SHA-256:
`59c91cad28726325d11cba8c9812e6e2f630fca9ded89c092ebc6d02d13648d8`.

The assertions describe desired behavior, so the failures below are intentional
diagnostic results against the reference implementation:

| Replay | Observed result | Source explanation |
| --- | --- | --- |
| Request watching before `READY` | **FAIL:** final activity is `Browsing Stuff` | `connect()` finishes after WebSocket setup, before authentication readiness; the update returns while disconnected. `READY` then submits browsing. `discord_rpc.dart:266–296,345–390,549–553`. |
| Request watching after `READY` | **PASS:** watching is sent | Same controller, media, and fake transport; changing event order changes the outcome. |
| Reconnect during a working watching activity | **FAIL:** watching becomes `Browsing Stuff` | There is no retained desired activity; every new `READY` publishes browsing. `discord_rpc.dart:184–187,384–390`. |
| Remote socket close during watching | **FAIL:** disconnected with no new connection or active retry timer | `onDone` clears the connected flag and cancels heartbeats, but schedules no recovery. Another UI event or lifecycle transition is needed. `discord_rpc.dart:349–359`. |
| Dispatch sequence 42, then heartbeat ACK | **FAIL:** next heartbeat carries `null` | Every packet replaces `_sequenceNumber`, including ACK packets without `s`. `discord_rpc.dart:373,393–395,437–442`. |
| Server requests an immediate heartbeat | **FAIL:** no response | The switch handles opcodes 10, 0, and 11 only. `discord_rpc.dart:375–397`. |
| Clear while a cover lookup is pending | **FAIL:** the old watching activity is sent after the clear | `clearPresence` bypasses the update queue; the in-flight update has no revision/cancellation check after its awaits. `discord_rpc.dart:141–154,607–649,1138–1152`. |
| Rapid mobile inactive → resumed | **FAIL:** old background cleanup closes the new connection | The background operation waits 150 ms and then disconnects whichever socket is current. Lifecycle operations are not serialized. `discord_rpc.dart:168–193`. |
| Desktop update triggers connection initialization | **FAIL:** update remains blocked | Desktop initialization awaits a browsing update queued behind the very update waiting for initialization. `discord_rpc.dart:141–154,288,322–325,549`. |

Commands executed included:

```sh
python3 /tmp/opencode/anymex-rpc-replay/run.py before-ready remote-close
python3 /tmp/opencode/anymex-rpc-replay/run.py after-ready reconnect-replay clear-during-image sequence-after-ack server-heartbeat remote-close lifecycle-overlap desktop-queue
```

The second command produced one passing assertion and seven failures. The
before-READY probe separately failed as described above. These reproduce local
causes of missing or incorrect activity; they do not establish which one occurred
on the user's device, or prove Discord's acceptance/rendering of a payload.

### Further source observations

- **Incomplete session recovery:** there is no handling for opcode 7 (Reconnect),
  opcode 9 (Invalid Session), `RESUMED`, `session_id`, `resume_gateway_url`, or close
  code classification. [AX-rpc, lines 345–443]
- **Serialization is not rate limiting:** `_enqueuePresenceUpdate` orders every
  request but neither deduplicates nor limits sends. Play/pause, duration, and
  navigation events can burst; trailing page debounce can postpone updates
  indefinitely during continuous scrolling. [AX-rpc, lines 141–154] [AX-player]
  [AX-reader]
- **Artwork is on the critical path:** updates await external-asset conversion;
  HTTP send and receive timeouts are configured, but failed URLs are not cached.
  This delays text presence and makes stale work more likely. [AX-rpc, lines
  445–501,607–608]
- **The mobile status label is misleading:** `Rich Presence Active` is selected
  from `isLoggedIn && isEnabled`, without checking `isConnected`. Logging
  “updated successfully” follows socket enqueue, not a rendering acknowledgment.
  [AX-settings, lines 301–315] [AX-rpc, lines 649–650]
- **Gateway timestamp units are already correct:** AnymeX uses milliseconds in
  Gateway activities. Do not copy the desktop library branch's conversion to
  seconds into AnimiX's Gateway payload. [AX-rpc, lines 622–625,658–661]
  [D-events, Activity Timestamps]

## Proposed Go implementation

### 1. A small, stateful presence service

Add `internal/discord` with a client, Gateway protocol types, and activity
formatting. Expose a small API such as `SetActivity`, `ClearActivity`,
`SetEnabled`, `Status`, and bounded `Close(context.Context)`.

`SetActivity` updates an in-memory immutable snapshot and returns promptly,
including while the Gateway is disconnected. A snapshot carries the playback or
reader session ID, revision, title, episode/chapter/page, optional public artwork
and AniList link, playback position/duration/rate, and observation time.

One owner goroutine manages connection state, desired activity, retry scheduling,
and writes. Use one maintained Go WebSocket transport on every desktop OS. Network
reads and optional artwork work report back with connection/session generations;
stale callbacks cannot mutate a replacement connection or publish old activity.

Use a bounded latest-value mailbox rather than an ever-growing FIFO. Reconnecting
publishes the latest desired state once ready; intermediate states are disposable.
`ClearActivity` and disable invalidate older work before any asynchronous cleanup.
Local activity tracking can continue while sharing is off, so enabling mid-episode
can publish the current snapshot without waiting for another play event.

Suggested observable states:

```text
disabled → needs_login → connecting → identifying/resuming → ready
                            ↑                               ↓
                            └──────── retry_wait ───────────┘
```

Authentication rejection transitions to `needs_login`; malformed configuration
has a distinct error. Report last heartbeat ACK, last write time, and close code.
“Connected / last activity sent” describes what the client can actually observe;
opcode 3 has no documented per-update rendering acknowledgment. [D-opcodes]

### 2. Complete the Gateway lifecycle

Implement the documented connection behavior, while keeping the unofficial
user-session payload details isolated: [D-gateway] [D-events] [D-opcodes]

- Bound dialing and Hello/Ready waits. A TCP/WebSocket connection is not `ready`.
- On Hello, schedule the first heartbeat with jitter using the supplied interval,
  then continue periodically. Respond immediately to a server opcode-1 request.
- Preserve the last **non-null dispatch sequence**. Track ACK receipt and latency.
- Cache `session_id` and `resume_gateway_url`. Resume recoverable disconnects with
  opcode 6; recognize `RESUMED`; re-identify when the session cannot be resumed.
- Handle opcode 7 immediately, opcode 9 according to resumability, remote close,
  read/write errors, missed ACKs, and machine sleep/wake. Transient failure retries
  use bounded exponential backoff with jitter; disconnect recovery is independent
  of future playback events.
- Treat 4004 as an authentication failure requiring new credentials; 4007/4009
  require a new session; 4008 requires slowing down. Respect non-reconnectable
  protocol/configuration close codes rather than looping indefinitely.
- Intentional shutdown and recovery need different close behavior: Discord
  documents 1000/1001 as invalidating a session, while resumable recovery preserves
  session information. Do not reuse a normal sign-out routine for every outage.
- Keep heartbeats/control traffic independent of slow HTTP artwork requests.
  Accommodate large incoming `READY` payloads; the documented 4096-byte limit is
  for outgoing events, not an appropriate incoming read limit.

### 3. Strict opt-in and truthful connection controls

Add an explicitly default-off setting, following the existing TOML settings
model:

```toml
[discord]
enabled = false
show_browsing = false
```

Disabled means no Discord dialing, profile requests, or artwork requests. A saved
credential alone does not enable sharing. Use an explicit account connection
action with masked token entry for the initial CLI integration; an AnymeX WebView
token scraper is not required for that flow. Account connection, enabling sharing,
and successful Gateway readiness are distinct states.

The current config package deliberately separates editable settings from managed
state. Add the preference to `Config`, `settings`, load/save, and migration paths;
adding only a field to `Config` would not persist it. The existing minimal
credential persistence is the user-only `0600` `state.json`, alongside the AniList
credential, with the same state conversion paths updated for a Discord token.
This is file-permission protection, not encrypted storage. [AM-config]

Disable/sign-out invalidates pending updates, best-effort clears the current
activity within the send budget, closes the session, and stops retries. Sign-out
also removes the credential. Connection diagnostics should contain state changes,
opcodes, timing, and close codes, rather than tokens or entire incoming events.

### 4. Live mpv observations outside Bubble Tea

This is the most important AnimiX-specific integration issue:

- `internal/player/mpv.go:81–154` creates a Lua script which observes position and
  duration, but writes progress on end-file/shutdown (or proxy failure).
- `Session.Progress`, lines 180–190, reads the result and removes the session
  directory; it is not a live-polling API.
- `internal/tui/model.go:2097–2107` runs the player with `tea.ExecProcess` and reads
  progress in the completion callback.
- Bubble Tea v1.3.10 explicitly blocks its program event loop during
  `ExecProcess`. TUI tick messages therefore cannot drive live playback presence.
  [AM-player] [AM-tui] [B-exec]

Extend `player.Session` with an mpv JSON IPC observer. mpv supports a Unix-domain
socket on Linux/macOS and a named pipe on Windows; these platform-specific player
connections feed the same Go activity API. Observe `time-pos`, `duration`,
`pause`, `paused-for-cache`, and `speed`, plus file-loaded, end-file, and shutdown
events. The IPC loop calls the concurrency-safe presence service directly while
the terminal program is suspended. [M-ipc]

Create the observer around the actual process lifetime, cancel it on exit or
launch failure, and invalidate its session ID before autoplay starts a replacement
player. Snapshot media metadata when launching; goroutines must not read mutable
TUI model fields. The existing final progress path still supplies local history
and tracker updates. mpv IPC carries no Discord credential.

When actively playing at rate `r`, with position `p` and duration `d` in seconds:

```text
start = now - p/r
end   = now + (d-p)/r
```

Serialize Unix milliseconds. Advance an older position observation to send time
using its observation timestamp and playback rate. Re-anchor after seeks, speed
changes, pause/resume, and buffering. While paused/buffering, remove the running
countdown and display a static state. Omit an end timestamp when duration is
unknown. Normal position ticks should not each become a Discord write.

### 5. Reuse reader events and available metadata

`mangareader.ProgressEvent` already carries `SessionID`, `Page`, and `Total`.
`BrowserReader.recordProgress` publishes latest progress through its channel;
the TUI consumes it in `mangaReaderProgressMsg` at `model.go:884`. Publish chapter
and page activity there, and clear it in `closeActiveMangaReader` at line 2378.
Reject observations from an obsolete session. [AM-reader] [AM-tui]

Today, closing only the browser tab emits no reader-close event. For tab-close
behavior, add browser liveness/visibility reporting and a bounded session lease;
a `pagehide` notification can accelerate cleanup but should not be the sole
signal. Lack of scrolling is not sufficient evidence that the reader has closed.
[AM-reader, lines 322–398]

`catalog.Media` contains source artwork, but `ContinueEntry` and the current
AniList `Entry` omit cover images; Resume also clears details and reconstructs
only title/URL. Use optional artwork with an application-asset fallback, and
retain/enrich cover metadata separately if needed. Use a verified AniList media
ID for an AniList button; source IDs/URLs are not AniList IDs. Register AnimiX's
own Discord application identity. [AM-models] [AM-config] [AM-anilist]
[AM-tui, lines 2012–2025]

### 6. Coalescing, artwork, and wire compatibility

Discord documents a maximum of **five activity updates per 20 seconds**, a
connection-wide budget of **120 Gateway sends per 60 seconds**, and outgoing
payloads of at most **4096 bytes**. Use a shared rolling-window limiter and
deduplication. A conservative normal refresh cadence of about five seconds is an
AnimiX policy; reserve capacity for clearing. A clear should replace pending
activity, not bypass the protocol's rate limits. [D-events] [D-gateway]

Text should be publishable without awaiting artwork conversion. Cache successful
lookups, back off failed ones, and accept late artwork only for the current
activity revision. Network failure in this service must not delay playback.

Do not blindly copy AnymeX's image/button wire format:

- AnymeX calls `/api/v9/applications/{id}/external-assets`, then sends `mp:` image
  paths and a string-label `buttons` array plus `metadata.button_urls`.
  [AX-rpc, lines 464–500,627–642]
- Current public activity docs describe sending external URLs directly, and
  sending buttons as `{label, url}` objects. The private conversion endpoint and
  `metadata.button_urls` are not documented there. [D-events, Activity Asset Image
  and Activity Buttons]

Begin compatibility testing with text and an AnimiX application asset, then add
external artwork and buttons individually. Public activity serialization is not
proof that the same shape is accepted on the unofficial user-session path.

## Wiring and implementation order

1. **Go service and protocol tests:** implement disabled/no-network behavior,
   connection state machine, latest activity retention, limiter, and diagnostics.
2. **Lifecycle and opt-in:** construct the service in `main.go`, pass it through a
   `tui.WithDiscordPresence` option following the existing option pattern, and
   add the account/settings action. Own bounded shutdown in the outer application
   lifecycle. Existing `fatal` uses `os.Exit`, so service-owning code should return
   errors through a scope whose defers run before fatal reporting. [AM-main]
3. **Live playback:** add the mpv observer and direct service updates independent
   of Bubble Tea, then cover startup, pause, seek, buffering, exit, and autoplay.
4. **Reader and presentation:** connect existing reader events, reader liveness,
   and optional browsing; add artwork/buttons after minimal activity is verified.

## Verification required for implementation

Use a local scripted Gateway and fake clock in Go to verify:

- Default-off makes zero network calls; a stored token does not override it.
- An activity submitted before `READY` is eventually the published activity.
- Reconnect/`RESUMED` publishes the newest state, including changes while offline.
- ACK packets preserve sequence; requested heartbeats are answered; missed ACKs,
  opcode 7/9, and recoverable closes recover; bad auth stops retries.
- Old socket callbacks, old playback sessions, and delayed artwork cannot
  overwrite or resurrect newer/cleared activity.
- Bursts of page, seek, and play/pause changes remain within the rolling limit;
  continuous input still gets periodic latest-state updates.
- Playback observation and Gateway heartbeats keep running while `ExecProcess`
  blocks the TUI; failed launch and all exit paths end the activity session.
- Unknown duration, pause, buffering, and non-1x speed produce sensible timestamps.
- Reader tab closure expires activity, without treating a stationary reader as
  closed; normal quit/sign-out performs bounded cleanup.

Finally verify rendering with a real account and an independent observer across
the targeted desktop platforms, including Discord desktop completely closed,
network interruption, sleep/wake, and another Discord session being open. This
live check is still needed: local socket-write success and a heartbeat ACK cannot
prove that other users see the intended Rich Presence.

## Sources

Discord and mpv web sources retrieved 2026-10-30. Source code links for the
reference are pinned to the audited revision.

- [D-gateway] [Discord Gateway lifecycle, limits, and recovery](https://docs.discord.com/developers/events/gateway).
- [D-events] [Discord Gateway Events, activities, timestamps, assets, buttons](https://docs.discord.com/developers/events/gateway-events).
- [D-opcodes] [Discord Gateway opcodes and close codes](https://docs.discord.com/developers/topics/opcodes-and-status-codes).
- [D-oauth] [Discord OAuth2 scopes and bot/user account distinction](https://docs.discord.com/developers/topics/oauth2).
- [M-ipc] [mpv manual: JSON IPC and property observation](https://mpv.io/manual/stable/#json-ipc).
- [B-exec] [Bubble Tea v1.3.10 ExecProcess implementation](https://github.com/charmbracelet/bubbletea/blob/v1.3.10/exec.go#L28-L51).
- [AX-rpc] [AnymeX Discord controller](https://github.com/RyanYuuki/AnymeX/blob/afbcc1b32ecf7110e31d305178a9a3f781e594ef/lib/controllers/discord/discord_rpc.dart).
- [AX-main] [AnymeX startup registration](https://github.com/RyanYuuki/AnymeX/blob/afbcc1b32ecf7110e31d305178a9a3f781e594ef/lib/main.dart#L267-L276).
- [AX-login] [AnymeX Discord login](https://github.com/RyanYuuki/AnymeX/blob/afbcc1b32ecf7110e31d305178a9a3f781e594ef/lib/controllers/discord/discord_login.dart).
- [AX-player] [AnymeX player presence callers](https://github.com/RyanYuuki/AnymeX/blob/afbcc1b32ecf7110e31d305178a9a3f781e594ef/lib/screens/anime/watch/controller/player_controller.dart).
- [AX-reader] [AnymeX reader presence callers](https://github.com/RyanYuuki/AnymeX/blob/afbcc1b32ecf7110e31d305178a9a3f781e594ef/lib/screens/manga/controller/reader_controller.dart#L425-L460).
- [AX-settings] [AnymeX Discord settings/status](https://github.com/RyanYuuki/AnymeX/blob/afbcc1b32ecf7110e31d305178a9a3f781e594ef/lib/screens/settings/sub_settings/settings_discord.dart#L294-L315).
- [AM-main] [AnimiX entrypoint](../../main.go#L107-L144).
- [AM-config] [AnimiX settings and managed state](../../internal/config/config.go#L303-L495).
- [AM-player] [AnimiX mpv session/progress](../../internal/player/mpv.go#L81-L190).
- [AM-tui] [AnimiX TUI integration](../../internal/tui/model.go).
- [AM-reader] [AnimiX browser reader events and lifecycle](../../internal/mangareader/reader.go).
- [AM-models] [AnimiX source media model](../../internal/catalog/models.go#L23-L34).
- [AM-anilist] [AnimiX AniList entry model](../../internal/anilist/client.go#L30-L49).
