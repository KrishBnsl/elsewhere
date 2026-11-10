# Elsewhere

An amber-on-black terminal player for music files on macOS, Linux and Windows.

### Install

One command installs Elsewhere and [Melisma](https://github.com/KrishBnsl/Melisma), the engine that chooses what plays next, and sets Melisma up.

macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/KrishBnsl/elsewhere/main/install.sh | sh
```

Windows (PowerShell, then run Elsewhere in Windows Terminal):

```powershell
irm https://raw.githubusercontent.com/KrishBnsl/elsewhere/main/install.ps1 | iex
```

It installs `elsewhere` and `dhunn` (to `~/.local/bin`, or `%LOCALAPPDATA%\Programs\Elsewhere` on Windows) and adds that folder to your PATH if needed. Then it asks who's listening, and offers to let Melisma listen to your Music folder once (about a second a song).

Ready-made programs exist for macOS on Apple Silicon, Linux (x86_64 and arm64) and Windows (x86_64), and need nothing else installed. Linux needs glibc 2.38 or newer (Ubuntu 24.04, Debian 13, Fedora 39, Arch or later) and ALSA, which desktop systems already have (PipeWire and PulseAudio provide it). On an Intel Mac, or with `sh -s -- --source`, the installer builds both from source instead, which needs Git, [Go](https://go.dev/dl) 1.25 or newer and [Rust](https://rustup.rs), plus a C compiler, `pkg-config` and the ALSA headers on Linux.

Options for `install.sh` (put them after `sh -s --` when piping):
- `--music <folder>`, `--name <name>`, `--bin-dir <folder>`;
- `--no-setup`, `--no-modify-path`, `--yes`;
- `--uninstall` removes the two programs and leaves your library, likes and history.

`install.ps1` takes the same options as `-Music`, `-Name`, `-BinDir`, `-NoSetup`, `-NoModifyPath`, `-Yes` and `-Uninstall`:

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/KrishBnsl/elsewhere/main/install.ps1))) -Music D:\Music
```

Run `elsewhere update` from your shell to install the latest release of Elsewhere and Melisma together. The updater checks the release bundle's SHA-256 and puts both binaries alongside the running executable. Close any open player before updating. Your music, session, playlists and Melisma data stay in place. Use `elsewhere --version` to check your build.

If your installed version predates the updater, rerun the installer above once a release includes it. On platforms without a prebuilt bundle, rebuild from source to update.

On macOS, Elsewhere plays through the system's own audio. On Linux and Windows, Melisma's `dhunn audio` plays the sound, measures the spectrum and reads tags and covers, so `dhunn` must stay installed alongside it. Linux and Windows don't show the system output volume. Chiptune Radio needs Game Music Emu (`brew install game-music-emu`, or `libgme0` on Linux) and isn't available on Windows.

To build Elsewhere alone from this folder, use Go 1.25 or newer and the macOS Command Line Tools for Xcode:

```sh
mkdir -p "$HOME/.local/bin"
go build -trimpath -ldflags='-s -w' -o "$HOME/.local/bin/elsewhere" .
```

Every push is built and tested on macOS, Linux and Windows by GitHub Actions (`.github/workflows/build.yml`), which also packages the downloads. Pushing a `v*` tag publishes them as a release:

```sh
git tag v0.2.0 && git push origin v0.2.0
```

`scripts/release.sh <tag>` still builds and publishes just the Apple Silicon download from this Mac. When testing on a Mac, `go test -tags dhunn_audio ./...` runs the Linux/Windows audio path through `dhunn audio`.

Once installed, run `elsewhere` from any directory, or pass a different library with `elsewhere "/path/to/your/music"`. The built executable uses macOS audio frameworks and runs without Go.

With no folder argument, Elsewhere scans `~/Music`. The library reads embedded titles, artists, album names, and album artists, including Vorbis comments in FLAC files. Tracks with the same tagged album and album artist can group across folders. Untagged files fall back to folder and filenames. Tracks play in natural path order. **Liked Songs** stays at the top of the library. Press `*` to like or unlike the selected track, or the current track in Now Playing. Likes persist locally between launches. Playlists appear below Liked Songs. Use `/np` then Enter to name a playlist. Tab moves to the optional description. Use `/ap` then Enter to open the fuzzy playlist picker for the selected or playing song. Type to filter, use the arrows to move, and press Enter to add. Playlist entries retain the order in which songs were added.

Starting a track opens a centered Now Playing popup. It checks for a local `cover`, `folder`, or `front` image (JPG or PNG) and embedded art. If neither is available and artist tags are present, it asks whether to download a cover. Press Y or Enter to search [MusicBrainz](https://musicbrainz.org/doc/MusicBrainz_API/Search/ReleaseGroupSearch) by album title and artist, then fetches the front cover from the [Cover Art Archive](https://musicbrainz.org/doc/Cover_Art_Archive/API). Elsewhere saves the approved cover as `cover.jpg` or `cover.png` beside the song and refreshes Now Playing during playback. Press N to skip or C to bring the prompt back. Existing covers are preserved. Kitty and Ghostty display native images in Now Playing when run directly. Other terminals and multiplexers use the colored text cover. Press Esc to browse while playback continues or M to reopen the popup. The volume row shows app volume and, when available, the Mac output volume. The amber spectrum reads the track being played and overlays the bars on falling characters. Press L to switch to synced lyrics. Elsewhere checks for a matching `.lrc` file next to the audio file, such as `Blue Hour.lrc` next to `Blue Hour.flac`. If there is no local file, it sends the track title, artist, tagged album when available, and duration to [LRCLIB](https://lrclib.net/docs) in the background. Successful results are cached as `.lrc` files under the user cache directory for offline use. The viewer highlights each timestamped line as it plays, for example `[00:12.50]The first line`.

### Melisma

Elsewhere can hand the choosing to [Melisma](https://github.com/KrishBnsl/Melisma), a recommender that learns your taste from the sound of your music and from how you listen. Elsewhere looks for `dhunn` beside its own binary, then on your PATH (`cargo install --path crates/dhunn-cli` in the Melisma folder), and starts it in the background as `dhunn engine`. The top bar shows how it's doing: starting, ready with how many songs it knows, choosing (with its mode and exploration level), stopped, or not installed. Set `MELISMA_ENGINE` to use a different executable, or to `off` to never start it.

Type `/melisma` to let it choose. If a song is playing, it keeps playing and Melisma carries on from it; otherwise its first pick starts now. Now Playing is then headed **MELISMA** with the mode and exploration level, and shows up to two reasons for each pick. When an album or the queue runs out, Melisma carries on from there even if you didn't ask. Choosing an album yourself hands the choosing back to you.

| Command | What Melisma does |
| --- | --- |
| `/melisma` | Chooses from here on |
| `/drift`, `/usual`, `/new` | Wanders gradually, stays with what you play most, or heads for music you've never played |
| `/toward name` | Travels in small steps toward an artist, album or song |
| `/vibe words` | Travels toward a described sound (needs `dhunn embed`) |
| `/friend name` | Travels into the part of a friend's taste you don't share yet |
| F / B / S / D in Now Playing | Further out, back toward home, a surprise, never again (also `/further`, `/back`, `/surprise`, `/never`) |
| `/q` | After your queue, what it would play if you listen straight through. Enter plays one now |
| `/taste` | What it has learned about you |
| `/analyze` | Listens to songs in this library it doesn't know yet, then restarts with them |

Songs Melisma hasn't analyzed show a `·` beside them in the track list. It ignores them until `/analyze` (or `dhunn analyze`) has run. It hears what you play, finish, skip, pause and like. Your likes from Elsewhere are shared when it starts, and un-liking a song tells it too. That history stays in Melisma's own library database, not in Elsewhere. Choosing another song yourself doesn't count as skipping the current one. Its messages go to `engine.log` and `analyze.log` in Elsewhere's cache folder.

### Radio

Type `/radio` and press Enter to open the station picker. Type to filter, use the arrows to move, and press Enter to tune in. Choose **Stop radio** from the picker to stop. **Anime FM** streams [LISTEN.moe](https://listen.moe/), and **Game OST** and **Remix Lab** stream [Rainwave](https://rainwave.cc/). **Vocaloid & Doujin** streams [R/a/dio](https://r-a-d.io/), a mix of Vocaloid, anime, doujin and game tracks. The live stations show track details and artwork when their feeds provide it. Anime FM falls back to the show or artist image when a song has no album cover, and R/a/dio looks covers up on MusicBrainz and falls back to the DJ's avatar. Space pauses a live station and reconnects to the live broadcast on resume.

Run `brew install game-music-emu` once to enable **Chiptune Radio**. Elsewhere shuffles [chiptune.app](https://chiptune.app/)'s game catalog, plays a supported tune, and lines up another game automatically. Press `<` to go back to the previous tune and `>` to go forward through the session history or pick a new game. Elsewhere keeps the last eight tunes until you choose **Stop radio**. Radio uses supported Nintendo, Sega, and VGM files from the catalog. Now Playing shows the game or console artwork from chiptune.app when available, with an amber arcade cabinet as the fallback. Local playback works without the decoder.

| Key | Action |
| --- | --- |
| ↑ / ↓ or j / k | Move the selection |
| Tab or ← / → | Switch between albums and tracks |
| Enter | Play the selected album or track, then continue through the album |
| Space | Pause or resume |
| / | Find music. A command list opens above the prompt: type to fuzzy-filter it, Tab or ↑/↓ to highlight a command, Enter to run it. Without a highlight, Enter keeps searching |
| : | Open the command list (`:q` quit, `:themes`, `:radio`, `:queue`). Same keys as `/` |
| /np, Enter | Create a playlist |
| /ap, Enter | Add the selected or playing song to a playlist |
| /radio, Enter | Open the fuzzy station picker |
| /themes, Enter | Pick a color theme. Moving previews it, Enter keeps it, Esc cancels. The choice is remembered |
| Esc | Close Now Playing or cancel an active search |
| m | Toggle Now Playing |
| v | Toggle the spectrum and rain in Now Playing |
| L | Open synced lyrics or switch back to Now Playing visuals |
| `>` / `<` | Next or previous track (`n` / `p` also work) |
| ← / → in Now Playing | Seek backward or forward 10 seconds (`[` / `]` also work) |
| - / + | Adjust app volume |
| * | Like or unlike the selected or playing track |
| q | Add the selected track (or album) to the queue, or remove it if already queued. In Now Playing it acts on the playing track |
| /q, Enter | Show the queue. Press `q` to remove the selected entry, Enter to play it now |
| :q, Enter | Quit |

### Playback and library shortcuts

- Elsewhere restores the last local track, position, queue and volume for each library. Press Space to resume.
- Press **g** or use **/find** to search songs, artists, albums and playlists together. Enter plays the result, Tab puts it next, and Right reveals it in the library.
- Press **N** to play the selection next or **Q** to append it. In **/queue**, J/K reorder manual entries, q removes one, X clears them and S saves the queue as a playlist.
- **/repeat** cycles through track, album and off. **/shuffle** shuffles the current collection. Albums use disc and track numbers where tagged.
- Consecutive tracks can transition gaplessly during sequential playback. On macOS, **/crossfade** opts into a roughly three-second blend.
- **/gain** cycles through track ReplayGain, album ReplayGain and off. **/output** opens the audio output picker. On macOS this changes the system output.
- Desktop media controls support play, pause, next and previous when available. On Linux and Windows, the newer player controls and output picker require a matching `dhunn` build.

### Listening modes

- **/tape** saves your five most recent local track plays in order. Use **/tape 10** for a longer tape. The cassette card shows the date and runtime, and saving creates a regular playlist.
- **/trip rainy neon** starts a Melisma journey toward a described sound (requires CLAP embeddings). **/trip to artist** heads toward a song, album, or artist. Now Playing shows the destination, route progress when available, and Melisma’s reasons. Use F, B, and S to steer.
- **/blind** hides the current song’s details, artwork, and lyrics in Now Playing. Press R to reveal the song. The next local track is hidden again until you toggle **/blind** off.
- **/timecapsule** resurfaces a local track you haven’t heard in at least 30 days or a forgotten like. The player shows the reason. It uses Elsewhere’s saved listening history and can also use matched history imported into Melisma.

### Themes

`/themes` offers **Amber** (default), **MAGI**, **Phosphor**, **Synthwave**, **Ice**, **Sakura** and **Mono**. Themes change colors, frame styles, labels and the spectrum animation. **MAGI** is modeled on the NERV MAGI system from Neon Genesis Evangelion: orange on black with slanted frames, hex rain, and a strip of three computers that vote on the current state. MELCHIOR·1 follows playback (承認 playing, 保留 paused, 否決 stopped), BALTHASAR·2 the like on the current track, and CASPER·3 the queue. The choice is saved in `Elsewhere/theme` in your config folder.

Run the terminal smoke test with `python3 tests/smoke.py`.
