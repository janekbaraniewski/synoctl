# synoctl

A TUI-first management tool for Synology DSM. Auto-discovers your NAS,
stores credentials in the macOS Keychain, and ships with structured
drill-downs for every list view (no JSON dumps in the UI).

## Quick start

```bash
make build         # → ./bin/synoctl
make run           # build + launch the TUI

# First run auto-onboards: mDNS scan → pick device → credentials.
# Subsequent runs jump straight to the dashboard.
```

## Tabs

| icon | tab | purpose | actions |
|------|-----|---------|---------|
| ◆ | **Dashboard** | Live CPU / memory / network / disk gauges, sparklines, per-volume bars, disk strip, top processes (by CPU), recent system-log activity. | `r` refresh |
| ▮ | **Volumes** | List of volumes. ⏎ opens a charted drill-down: capacity gauge, inode gauge, full properties, capabilities chips, health suggestions, contributing disks. | ⏎ details · `/` filter |
| ● | **Disks** | Physical drives. ⏎ opens a temperature gauge with thermal banding, SMART chip, pool-membership list, full properties. | ⏎ details · `/` filter |
| ▦ | **Shares** | Shared folders. ⏎ opens a quota gauge + flag chips (encrypted / recycle / hidden / read-only / USB / sync / cloud-sync). | ⏎ details · `/` filter |
| 🗁 | **Files** | File Station browser — every share + drill-down navigation. ⏎ on a folder navigates in; ⏎ on a file opens an inspector (size, perms, owner, mtime/atime/ctime/crtime). | ⏎ open · ⌫/`h` up · `N` rename · `D` delete (confirm) |
| ◐ | **Users** | Local DSM accounts. ⏎ opens account flags, properties, and group memberships. | ⏎ details · `/` filter |
| ▣ | **Packages** | Installed packages. ⏎ opens a wrapped description, flag chips, full properties. | `s`/`x`/`R` start/stop/restart · `U` uninstall (confirm) |
| ⌬ | **Services** | DSM services. ⏎ shows togglability + properties. | `e`/`d` enable/disable |
| ⇄ | **Network** | Interfaces. ⏎ shows addressing block. | ⏎ details |
| ⌂ | **System** | Identity (model, serial, CPU, RAM, NTP, time zone), Runtime (load averages, memory, swap, buffer/cache), Power. | `B` reboot · `S` shutdown — both confirm |
| ≡ | **Logs** | Paginated system / connection log. ⏎ opens entry with description. | `n`/`p` next/prev · `t` toggle source |

## Global keys

| key | action |
|---|---|
| `tab` / `[` `]` | next / previous view |
| `:` | command palette |
| `/` | filter in list views |
| `r` | refresh now |
| `?` | help overlay |
| `q` | quit |

## CLI subcommands

| command | what it does |
|---|---|
| `synoctl` | Launch the TUI (auto-onboards on first run). |
| `synoctl discover` | mDNS scan only. |
| `synoctl login` | Re-run onboarding to add/update a profile. |
| `synoctl logout` | Remove the active profile's password from Keychain. |
| `synoctl apis [-f X]` | Dump SYNO.API.Info — what's actually advertised by your DSM build. |
| `synoctl raw <api> <method> [-v N] [-p k=v]` | Issue any DSM call and print the JSON envelope. Indispensable for diagnosing API mismatches across DSM versions. |
| `synoctl version` | Build info. |

## How it stays version-tolerant

DSM API naming + payload shapes drift between firmware versions. The
verified shapes for everything here came from a DS220j on DSM 7.0.1-42218
introspected with `synoctl apis` and `synoctl raw`. Where DSM is
inconsistent (eg `recycle_bin_admin_only` is sometimes bool, sometimes
int), we use a `flexBool` decoder that accepts both forms.

## What's intentionally not wired

* Package install (multi-step download → install flow against
  SYNO.Core.Package.Server / SYNO.Core.Package.Installation).
* User edit / create / delete (SYNO.Core.User set/create need round-trip
  of every field; safer behind a real form).
* Snapshot listing (SYNO.Core.Share.Snapshot requires 2FA step-up per
  call on this firmware — UX needs a refresh-OTP modal first).
* File download (requires saving to disk; not in scope for v1).

## Project layout

```
cmd/synoctl/                 # binary entry
internal/cli/                # Cobra commands + onboarding flow
internal/config/             # YAML config + Keychain wrapper
internal/discover/           # mDNS scanner
internal/dsm/                # Typed DSM Web API client (one file per area)
internal/tui/                # Bubbletea root model, theme, keymap
internal/tui/views/          # Per-screen views + shared listBase + Confirm + Prompt
```
