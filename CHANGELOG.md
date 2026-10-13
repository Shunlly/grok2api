# Changelog

Shunlly/grok2api fork of [chenyme/grok2api](https://github.com/chenyme/grok2api).

## v3.1.7 — 2026-10-13

### Fixed
- **Codex 502 root cause**: the Codex model catalog now sets `auto_compact_token_limit`
  (85% of the context window, capped at 300k tokens, matching the official grok CLI's
  `auto_compact_threshold_percent = 85`). Codex sessions previously grew to ~330k+ tokens /
  ~3 MB request bodies, which `cli-chat-proxy.grok.com` answered with Cloudflare "Host Error" 502
  after 100–225 s, and Codex kept resending the same oversized request.
- Fail fast on Cloudflare HTML 5xx and HTTP/2 `PROTOCOL_ERROR` with distinct error codes; soft
  cooldown only (#3).
- `/v1/responses` local context-length precheck returning `context_length_exceeded` (#3).
- Default `routing.maxAttempts` / `videoMaxAttempts` 999 → 5 (#3).
- Quality guard no longer misjudges `grok-4.20-multi-agent` (chenyme#1058) (#2).
- Optional auto-disable of degraded accounts (`qualityGuard.requestRetry.autoDisable`, default off) (chenyme#1052) (#2).
- Voice/STT WS request timeout and STT billing (chenyme#1071, #1072); video retry key scope (chenyme#1066);
  Build 403 cooldown (chenyme#1064); SSO→Build `consent_token` (chenyme#1059) (#1).

### Changed
- Recommended Grok Build client version 1.0.40 → 1.0.50 (chenyme#1068) (#1).
- Links, update check and release URLs now point to Shunlly/grok2api.
