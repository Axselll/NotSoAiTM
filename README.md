# sessmove

Save a logged-in browser session on one machine and restore it in a browser on another.

> `state.json` holds live session cookies. Treat it like a password: never commit, email, or chat it; move it over USB or AirDrop; delete it when done.

## Requirements

- Go 1.25+ (to build)
- A Chromium-based browser installed where you run it: Chrome, Chromium, Edge, Brave, Opera, or Vivaldi. (Firefox and Safari won't work.)
- chromedp finds the browser on `PATH`. If yours isn't named `google-chrome`, symlink it into a `PATH` directory under that name.

## Build

```bash
mkdir sessmove && cd sessmove      # put main.go here
go mod init sessmove
go get github.com/chromedp/chromedp@v0.21.0 github.com/chromedp/cdproto@v0.157.8
go build -o sessmove .
```

Cross-compile a binary for the other machine (no Go needed there):

```bash
GOOS=windows GOARCH=amd64 go build -o sessmove.exe .    # Windows
GOOS=darwin  GOARCH=arm64 go build -o sessmove .        # Apple Silicon Mac
GOOS=darwin  GOARCH=amd64 go build -o sessmove .        # Intel Mac
```

## Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `-mode` | — | `save` or `restore` (required) |
| `-url` | — | Absolute site URL, e.g. `https://example.com/` (required) |
| `-state` | `state.json` | State file path |
| `-cookie` | — | Auth cookie name to require on save (optional but recommended) |
| `-profile` | — | Persistent browser profile dir; session survives restarts (optional) |
| `-timeout` | `5m` | Overall timeout |

## Use

### 1. Save (your machine)

```bash
./sessmove -mode save -url https://SITE/ -state state.json -cookie SESSION_COOKIE_NAME
```

A browser window opens. Log in there, then press **Enter** in the terminal.

- If the named `-cookie` isn't present, it errors and writes nothing (so you can't save a pre-login session by pressing Enter too early).
- Without `-cookie`, it only checks that at least one cookie exists.

To find the cookie name: in the logged-in site, DevTools → Application → Cookies, and look for the one that identifies your session.

### 2. Move the files

Copy `sessmove`/`sessmove.exe` and `state.json` to the other laptop over USB or AirDrop.

### 3. Restore (other machine)

```bash
# ephemeral window
./sessmove -mode restore -url https://SITE/ -state state.json

# OR persist across restarts
./sessmove -mode restore -url https://SITE/ -state state.json -profile ./sis-profile
```

A browser window opens already logged in and stays open after the program exits. Use that window (new tabs in it share the session). The other machine's normal browser is unaffected.

### 4. Clean up

Log out from the restored window (kills the session server-side), then delete `state.json` on both machines and the profile directory the program prints.

## Limits

- **Single domain only.** Copies cookies + localStorage for that one origin. SSO identity-provider cookies, IndexedDB, and service workers are not copied.
- **`sessionStorage` is best-effort** (per-tab; restore isn't guaranteed).
- **Secrets are plaintext** in `state.json`. `0o600` protects it on Linux/macOS; on Windows it inherits folder permissions.
- **Many sites will reject a moved session**: those that bind sessions to device/IP, use device-bound credentials, or force MFA on a new device. Banks and big providers (Google, Microsoft) commonly do. If restore lands on the login page, the site won't allow it and the fix is to log in normally.
- **Hardened/enterprise browsers** (e.g. Prisma Access Browser) disable remote debugging, so chromedp can't drive them.
- **Not for work/managed accounts.** Lifting a corporate session onto a personal device bypasses the controls it exists to enforce.
