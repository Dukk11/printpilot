# Changelog

All notable changes to this project will be documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-09-11

### Added
- Bambu Lab connector: LAN MQTT (Developer Mode), normalized status mapping,
  automatic reconnect, trust-on-first-use TLS certificate pinning.
- Klipper connector: Moonraker HTTP polling with offline detection after 3
  failed polls.
- Mock fleet (`--mock`): four simulated printers covering every dashboard
  state — for demos, screenshots and first runs.
- Dashboard: single-page embedded UI (no CDN, no build step), live progress,
  temps, layers, webcam snapshots, state-colored cards, dark mode,
  `prefers-reduced-motion` support.
- Alerts: Telegram and generic webhook, persisted once-per-job dedup for
  `failed`, `finished` and `offline` events.
- Config validation with actionable error messages.
- CI: gofmt/vet/test pipeline; tagged releases via GoReleaser.
