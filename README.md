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
- [Fyne v2](https://fyne.io/) for the GUI
- [fclairamb/ftpserverlib](https://github.com/fclairamb/ftpserverlib) for the FTP server
- [spf13/afero](https://github.com/spf13/afero) for filesystem abstraction and path sandboxing
- [fsnotify/fsnotify](https://github.com/fsnotify/fsnotify) for activity detection
- [natefinch/lumberjack](https://github.com/natefinch/lumberjack) for log rotation
- [jlaffaye/ftp](https://github.com/jlaffaye/ftp) (test-only) as the FTP client in end-to-end tests

## Source layout

- `main.go` — everything cross-platform: config, driver, GUI, main loop
- `singleton_windows.go` — Windows-only named mutex + window focus (build tag `windows`)
- `singleton_other.go` — no-op stubs for other OSes (build tag `!windows`)
- `main_test.go` — unit tests for pure helpers
- `e2e_test.go` — end-to-end tests spinning up a real FTP server on 127.0.0.1

## Build

Requirements: Go 1.21+ and `gcc-mingw-w64-x86-64` (CGO toolchain for
cross-compiling Fyne to Windows).

```bash
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)

CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 \
  go build \
    -ldflags "-H windowsgui -s -w -X main.version=$VERSION" \
    -o auto-ftp.exe .
```

- `-H windowsgui` — no console window at startup
- `-s -w` — strip debug/symbols (lighter binary)
- `-X main.version=$VERSION` — inject the version string; appears in the
  GUI footer and in the first log line

### Per-site credentials

`ftpUser` and `ftpPass` default to `vmsync` / `vmsync` but can be
overridden at build time, letting you produce a binary per site without
editing the source:

```bash
go build \
  -ldflags "-H windowsgui -s -w -X main.version=$VERSION -X main.ftpUser=siteA -X main.ftpPass=strong-secret" \
  -o auto-ftp-siteA.exe .
```

Port, PASV range, folder name, and app name remain compile-time
constants; edit them in `main.go` and rebuild if you need to change
them.

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

```
                    +---------------------+
                    |   main()            |
                    |   - setupLogger     |
                    |   - singleton guard |
                    |   - Fyne app        |
                    +-----+---------------+
                          |
                          v
+-----------+    +------------------+     +-------------------+
| fsnotify  |--> | activityTracker  |     | ftpserverlib      |
| watcher   |    | (GUI label)      |     |   <- driver       |
+-----------+    +------------------+     |   <- afero.BasePF |
                                          +-------------------+
                                                  ^
                                                  |
                                          +-------+--------+
                                          | heartbeat loop |
                                          | (30s TCP dial) |
                                          +----------------+
```

The Fyne GUI is wired to server state through three feedback paths:

1. **Bind result** (synchronous in `main`) — sets the initial badge
   state.
2. **`server.Serve()` goroutine** — if it returns while the server was
   meant to be running, the badge flips via `fyne.Do`.
3. **Heartbeat goroutine** — dials the loopback listener every 30 s,
   requires two consecutive failures before flipping the badge
   (debounce), restores the online state on the first successful dial.

A `context.Context` cancelled by `onClose()` stops all background
goroutines; the `stopped` atomic bool ensures `server.Stop()` runs
exactly once.

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
git tag -a v0.4.0 -m "v0.4.0 — <summary>"
git push origin v0.4.0

# build against the tag
VERSION=v0.4.0
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 \
  go build -ldflags "-H windowsgui -s -w -X main.version=$VERSION" \
    -o auto-ftp.exe .

gh release create v0.4.0 auto-ftp.exe \
  --target main \
  --title "v0.4.0 — <summary>" \
  --notes "<release notes>"
```
