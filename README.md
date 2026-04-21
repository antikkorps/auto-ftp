# Auto-FTP for EasyVIEW

A zero-config, portable local FTP server packaged as a single Windows
executable. Built to receive files from **EasyVIEW** running in a
VirtualBox VM, so they can then be synced from the host PC (USB stick,
backup script, etc.).

## Purpose

The on-site staff is non-technical and has no admin rights. The tool
must work with a **single double-click**, without configuration, and
restart automatically after a reboot.

## How it works

On launch, `auto-ftp.exe`:

1. Creates a `graphiques/` folder next to the executable (if missing)
2. Creates a `logs/` folder next to the executable for log files
3. Drops an `auto-ftp.vbs` into the user's `Startup` folder so the app
   relaunches at next Windows boot (silent, no admin rights required,
   reversible by deleting the VBS)
4. Starts an FTP server on `0.0.0.0:2121` (PASV on 2122-2130)
5. Opens a window displaying the settings to enter in EasyVIEW (IP,
   port, user, password), each with a **Copy** button

Closing the window or clicking **Stop server** shuts the server down
cleanly.

## Hardcoded credentials

| Setting        | Value    |
| -------------- | -------- |
| Port           | `2121`   |
| User           | `vmsync` |
| Password       | `vmsync` |
| Target folder  | `graphiques/` (next to the exe) |

To change a value: edit the constants at the top of `main.go` and
recompile.

## Build (cross-compile Linux → Windows)

Requirements:

- Go 1.21+
- `gcc-mingw-w64-x86-64` (CGO required by Fyne)

Command:

```bash
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 \
  go build -ldflags "-H windowsgui -s -w" -o auto-ftp.exe .
```

- `-H windowsgui`: no console window at startup
- `-s -w`: strip symbols (smaller binary)

The resulting binary is ~25 MB and has no runtime dependency (fully
static except for Windows system DLLs).

## Tests

```bash
go test ./...
```

## Deployment

1. Copy `auto-ftp.exe` into a folder on the host PC (e.g.
   `C:\EasyVIEW-FTP\`)
2. Double-click → Windows will prompt for network authorization on
   first launch; click **Allow access** (include both public and
   private networks)
3. Configure EasyVIEW in the VM with the values shown on screen

On next Windows boot, the app relaunches automatically.

## Logs

A rotating log is written to `logs/auto-ftp.log` next to the executable
(5 MB × 3 backups, 30-day retention). It records:

- Server start / stop
- Client connections (remote IP)
- Authentication attempts (successes and failures, with attempted user)
- FTP commands and transfers
- Errors

The **View logs** button in the window opens the file directly in
Notepad — useful for remote support: *"open the View logs button and
read me the last line."*

## Security notes

This tool is designed for **isolated or trusted LANs only**:

- **Plaintext FTP** — credentials and file contents travel unencrypted.
  Do not expose port 2121 to a hostile network.
- **Hardcoded weak credentials** (`vmsync` / `vmsync`) — acceptable on
  a private LAN where only the VM connects, unacceptable on a shared
  or public network.
- **Port bound to `0.0.0.0`** — any host on the LAN can attempt to
  connect. Combine with Windows Firewall rules or a dedicated
  host-only VirtualBox network if this is a concern.
- **Path confinement** — client writes are sandboxed to `graphiques/`
  via `afero.BasePathFs`, preventing escape via relative paths.

If you need to raise the bar, the next steps would be: enable TLS
(FTPS), replace the hardcoded password with a generated one shown on
first run, or switch to SFTP.

## Uninstall

- Delete `auto-ftp.exe`, `graphiques/`, and `logs/`
- Delete `%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\auto-ftp.vbs`

## Stack

- **Go 1.25** + CGO
- [Fyne v2](https://fyne.io/) for the GUI
- [fclairamb/ftpserverlib](https://github.com/fclairamb/ftpserverlib) for FTP
- [spf13/afero](https://github.com/spf13/afero) as filesystem abstraction
- [lumberjack](https://github.com/natefinch/lumberjack) for log rotation
