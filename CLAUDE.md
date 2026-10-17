# CLAUDE.md

## Project

AgentMeter (formerly Codex Monitor) is a Windows x64 application written in Go that monitors local Codex and Claude Code usage. MyGO renders the main window; Go/Win32 renders the transparent desktop card and token orb. Both share a single in-process data service. UI copy defaults to Simplified Chinese and supports English. There is no browser, Electron, React, Vite, npm dependency, or separate widget process.

## Commands

Use PowerShell 7 and Go 1.27.1. `scripts/go.cjs` resolves GO_EXE, the local Windows Go installation or PATH. npm scripts require Node 22.19+ and use only its standard library.

- `go run .` / `npm start` / `npm run dev`: launch the native app.
- `npm run build`: produce the executable and icon in build/native (release build with `-tags mygo_noinspector`).
- `go test ./...` / `npm test`: data, recovery, native UI and widget tests.
- `npm run native:check`: synthetic native-window acceptance, including all five pages and the real Win32 widget.
- `npm run widget:check` / `npm run test:ui`: aliases for the native acceptance suite.
- `npm run package`: build the NSIS/LZMA installer and SHA-256 checksum; requires NSIS 3 and Unicode nsProcess (MAKENSIS / NSIS_PLUGIN_DIR or existing cache).
- `npm run package:check`: isolated installer acceptance, including old runtime cleanup, file hashes, shortcuts, registry, installed service, database repair, running-app guard, upgrade and uninstall.

Close the monitor from its tray before replacing an executable; GUI close hides to tray. Distribute build/native, which contains only Codex Monitor Native.exe and icon.ico. The executable, MyGO app name/identifier, data directory (%APPDATA%\codex-monitor), login entry and uninstall ID intentionally keep their 2.x names so upgrades and signed in-place updates work; user-visible branding comes from monitor.AppName. No runtime directory is needed.

## Architecture

- main.go embeds icons/locales and starts internal/nativeapp.
- internal/nativeapp owns pages, tray, window behavior, transitions and export. theme.go holds the light/dark system palettes, fonts, spring motions and shared controls (card, header, badge, segmented, popup, group/row); shell.go holds the sidebar, glass toolbar, filters and command palette. Build new UI from these helpers instead of styling elements ad hoc.
- widget*.go owns widget layout, formatting, animation, refresh coalescing and per-pixel-alpha Win32 presentation. Its callbacks and model run on the GUI thread; asynchronous service results return through the GUI dispatcher. Keep memory-renderer/font caches across frames.
- internal/monitor owns SQLite, source parsing/scans, accounts, prices, statistics, quotas and actions. One serialized service owns the database. scan.go registers one collector per source (Codex, Pi, Claude Code); claude.go parses Claude Code transcripts and its cached quota. Every usage/turn/session/quota row carries an agent (codex/claude); the agent filter flows through snapshots, activity, calls and export.
- internal/testfixture and cmd/fixture generate temporary synthetic acceptance profiles. Never use personal data in test captures or release assets.
- scripts/legacy-*-files.nsh contains only removal lists for upgrading prior installations, not any old runtime code.

See docs/architecture.md for data contracts and maintenance rules.

## Invariants

Do not write Codex/Pi/Claude Code session, config or diagnostic source files or persist message bodies, tool calls, authentication tokens or API keys. Never open Claude Code's .credentials.json; Claude quota comes only from cachedUsageUtilization in .claude.json. The enabled local TTFT collector may manage only Codex OTel settings and the bundled Pi extension, with configuration backup, ownership checks and preservation of unrelated settings. Offline mode must not modify client files. Preserve unknown values, response-ID deduplication, clear-history boundaries, account attribution and manual price versions. Widget usage covers all local accounts while quota belongs to one agent's current account (widgetQuotaAgent, auto by default). The widget never opens SQLite.

Preserve the fixed 560x380 DIP window, 536x356 card, 120x120 orb, CSS-equivalent timings/geometry, language and tooltip semantics, drag threshold, click suppression, transparent hit testing, keyboard focus, right-click menu, topmost choice, position and mode persistence. Respect reduced motion/transparency. Reassert fixed physical dimensions on every placement and DPI change. Release timers, pointer capture, tooltips and GDI objects on close; cancel pending requests.

All tests use synthetic profiles. Keep build, artifacts and databases out of Git. Retain .git; do not publish without user authorization.

Package only the native executable and icon. Install per-user, refuse replacement while running, and preserve the profile. Delete only known shipped legacy files and empty directories; never recursively delete an arbitrary installation directory. Installer acceptance uses separate names, paths and registry keys so an installed user application stays untouched.
