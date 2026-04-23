# auto-ftp — developer guide

Zero-config local FTP server packaged as a single Windows executable,
designed to receive files from EasyVIEW running in a VirtualBox VM.

**For on-site installation, deployment, and troubleshooting, see
[`DEPLOYMENT.md`](./DEPLOYMENT.md)** — that file is the right target for
the operator on site and for phone support.

This README is the technical reference for building, testing, and
extending the tool.

## Stack

- **Go** (currently 1.25) + CGO
- [Wails v2](https://wails.io/) — Go backend + WebView2-hosted HTML/JS
  frontend. Requires the Microsoft WebView2 Runtime on the target
  Windows machine (preinstalled on Win10 ≥ 21H2 and Win11; see
  `DEPLOYMENT.md` for legacy hosts)
- [fclairamb/ftpserverlib](https://github.com/fclairamb/ftpserverlib) for the FTP server
- [spf13/afero](https://github.com/spf13/afero) for filesystem abstraction and path sandboxing
- [fsnotify/fsnotify](https://github.com/fsnotify/fsnotify) for activity detection
- [natefinch/lumberjack](https://github.com/natefinch/lumberjack) for log rotation
- [jlaffaye/ftp](https://github.com/jlaffaye/ftp) (test-only) as the FTP client in end-to-end tests

## Source layout

- `main.go` — entry point: logger, Win32 singleton pregate, `wails.Run`
- `app.go` — `App` type bound to the Wails frontend: FTP lifecycle
  (`OnStartup`/`OnBeforeClose`), state snapshot, JS-callable methods
  (`GetState`, `ChangeFolder`, `OpenFolder`, `OpenLogs`, `QuitApp`),
  runtime event emission (`activity`, `status`)
- `ftp.go` — ftpserverlib driver: settings, auth, sandboxed FS, bind-error classification
- `config.go` — paths helper (`exeDir`, `dataDir` → `./af-data/`),
  `auto-ftp.cfg` load/save, lumberjack logger setup, Windows autostart VBS
- `watcher.go` — fsnotify watcher and heartbeat TCP dial goroutines
- `singleton_windows.go` — Win32 `CreateMutexW` pregate + `FindWindowW` refocus (build tag `windows`)
- `singleton_other.go` — no-op stubs for other OSes (build tag `!windows`)
- `frontend/` — Wails frontend (plain HTML/CSS + a single `main.js` module bundled by Vite)
  - `index.html`, `src/style.css`, `src/main.js` — the entire UI
  - `wailsjs/` — auto-generated JS bindings for Go methods, do not edit
- `main_test.go` — unit tests for pure helpers
- `e2e_test.go` — end-to-end tests spinning up a real FTP server on 127.0.0.1

## Build

Requirements: Go 1.21+, `gcc-mingw-w64-x86-64` (CGO toolchain for
cross-compiling to Windows), Node.js (for the Wails frontend bundler),
and the [Wails v2 CLI](https://wails.io/docs/gettingstarted/installation)
(`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

Run from the repo root:

```bash
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
  wails build -platform windows/amd64 \
    -ldflags "-X main.version=$(git describe --tags --always --dirty)" \
    -nsis=false
```

Output: `build/bin/auto-ftp.exe`.

- `-platform windows/amd64` — cross-compile target
- `-ldflags -X main.version=…` — inject the version string; appears in
  the GUI footer and in the first log line
- `-nsis=false` — skip the NSIS installer build (we ship the raw `.exe`)

### Per-site credentials

`ftpUser` and `ftpPass` default to `vmsync` / `vmsync` but can be
overridden at build time, letting you produce a binary per site without
editing the source:

```bash
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
  wails build -platform windows/amd64 \
    -ldflags "-X main.version=$(git describe --tags --always --dirty) -X main.ftpUser=siteA -X main.ftpPass=strong-secret" \
    -nsis=false
```

Rename `build/bin/auto-ftp.exe` per site if you produce several
variants. Port, PASV range, folder name, and app name remain
compile-time constants; edit them in `ftp.go` / `main.go` and rebuild
if you need to change them.

## Tests

```bash
go test ./...
```

- Unit tests (`main_test.go`) cover: credential check, settings
  construction, TLS-disabled behaviour, humanized durations, bind
  error classification.
- End-to-end tests (`e2e_test.go`) start a real FTP server on a
  random loopback port and exercise it with the jlaffaye/ftp client:
  wrong credentials rejected, successful login + upload round-trips
  bytes correctly, path traversal is confined to the sandbox.

## Architecture at a glance

```bash
                +------------------------------+
                |   main()                     |
                |   - setupLogger              |
                |   - Win32 singleton pregate  |
                |   - wails.Run(App{...})      |
                +-------+----------------------+
                        |
                        v
          +-----------------------------+
          | Wails runtime (WebView2)    |
          | <-> frontend/src/main.js    |
          +-------------+---------------+
                        | OnStartup
                        v
          +-----------------------------+
          | App (app.go)                |
          |   - bound methods (GetState,|
          |     ChangeFolder, QuitApp…) |
          |   - EventsEmit(activity,    |
          |     status)                 |
          +-+---------+-----------+-----+
            |         |           |
            v         v           v
     +-----------+  +------+  +-------------------+
     | fsnotify  |  | hbt  |  | ftpserverlib      |
     | watcher   |  | 30 s |  |   <- driver       |
     +-----------+  +------+  |   <- afero.BasePF |
                              +-------------------+
```

The WebView2 frontend is wired to server state through two paths:

1. **Bound method `GetState()`** — synchronous pull the frontend does
   at load to render initial badge, IPs, folder, last-file label.
2. **`wailsruntime.EventsEmit`** — push updates for `activity` (file
   landed in the watched folder) and `status` (online/offline flips).
   The heartbeat goroutine dials the loopback listener every 30 s and
   requires two consecutive failures before flipping the badge
   (debounce); first success restores it.

A `context.Context` is cancelled by `App.shutdown()` (called from
`OnBeforeClose` or from the frontend's `QuitApp`) and stops all
background goroutines. The `stopped` atomic bool ensures
`server.Stop()` runs exactly once even if both paths fire.

## Security posture

This tool is designed for **isolated or trusted LANs only**:

- Plaintext FTP — no TLS; credentials and file contents travel
  unencrypted.
- Hardcoded credentials at build time — acceptable on a private LAN
  where only the VM connects; otherwise override per site via
  `-ldflags -X` (see above) or switch to FTPS.
- Port bound to `0.0.0.0` — any host on the LAN can attempt to
  connect. Combine with Windows Firewall rules or VirtualBox
  host-only networking if this is a concern.
- Path confinement — client writes are sandboxed to the
  `graphiques/` folder via `afero.BasePathFs`; end-to-end tests
  cover this.

## Releases

Manual release flow (until a CI pipeline is added):

```bash
git tag -a v0.5.0 -m "v0.5.0 — <summary>"
git push origin v0.5.0

# build against the tag
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
  wails build -platform windows/amd64 \
    -ldflags "-X main.version=$(git describe --tags --always --dirty)" \
    -nsis=false

gh release create v0.5.0 build/bin/auto-ftp.exe \
  --target main \
  --title "v0.5.0 — <summary>" \
  --notes "<release notes>"
```
