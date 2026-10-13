# Changelog

Shunlly/grok2api fork of [chenyme/grok2api](https://github.com/chenyme/grok2api).

## v3.2.1 — 2026-10-13

### Added
- **共享用量池调度**：Build 账号账单快照 `creditUsagePercent`（SuperGrok 周共享用量池）或上游额度窗口 `usagePercent` ≥ 90% 且快照不超过 6 小时时，选号排序降到健康账号之后，尽量在撞 429 前让出流量（仍可用，只是后选）。面板原有的周用量百分比与重置时间继续显示。
- **单模型额度封锁可见 + 解除**（上游 #1061）：账号列表与详情返回 `modelQuotaBlocks`（模型、原因、到期时间），面板额度列显示「N 个模型封锁」及明细，操作菜单新增「解除模型封锁」；新接口 `DELETE /api/admin/v1/accounts/:id/model-quota-blocks[?model=]`。
- **自动压缩阈值按上游目录校准**：读取 Build 模型目录的 `auto_compact_threshold_percent`（已读取的 `context_window` 继续生效），用于 Codex 目录 `auto_compact_token_limit`；缺省仍为 85%，300k 上限不变。
- 代理导入支持 `IP:端口:用户名:密码`（上游 PR #1069）。

### Fixed
- Web Imagine 按 requestId 过滤 WebSocket 帧，修复返回历史旧图/串帧（上游 PR #1056）。
- 上游 HTTP 426（CLI 版本过低，#1068）返回明确错误 `upstream_client_outdated`，提示更新 ClientVersion，并且不再换号重试。

### Changed
- 金丝雀探测默认间隔 2h → 6h。

## v3.2.0 — 2026-10-13

### Changed
- **降智判定降级**：soft/hard/burst 吞吐分类改为信息性「快速输出」，不再计入降智次数/时序 severe、不再触发自动停用；只有缺失思考计入降智。吞吐改为只按可见输出（output − reasoning tokens）计算，修正上游先静默推理、再一次性刷出答案造成的误报。面板文案同步更新。
- Build `ResponseHeaderTimeout` 代码默认值 5m → 20m（与生产一致）。

### Added
- **账号金丝雀探测（仅观察）**：定时（默认 2h，`canary.*` 可配，默认关闭）向每个启用的 Build 账号固定发送轮换题目（确定答案的推理题 / SVG 结构题），stream + reasoning high + 输出上限；按号池思考 token 中位数、正确性/有效性、可见长度打分；连续 3 次失败且号池其余账号通过才标记可疑；传输错误与隐藏思考模型不计。新表 `canary_probe_results`、管理 API `GET /api/admin/v1/quality/canary`、`POST /api/admin/v1/quality/canary/run`，质量守护新增「金丝雀探测」标签页。结果不影响路由/冷却/停用。

## v3.1.9 — 2026-10-13

### Fixed
- SSE keepalive (v3.1.8): after an early 200 commit, stream chunks that began mid-event were mistaken for
  JSON errors and a spurious `response.failed` was injected. Only the first body write can now be converted.
  Header/status bookkeeping is serialized with the keepalive goroutine (race-detector clean).

## v3.1.8 — 2026-10-13

### Fixed
- `/v1/responses` context precheck no longer rejects valid large requests: lower-bound estimate now uses
  6 chars/token, compares against 1.5x the larger of the catalog/static context window, and skips bodies
  under 1 MiB (a ~250k-token request was wrongly rejected at an estimated 315k vs the 256k catalog window).

### Added
- SSE keepalive (`: keepalive` comments every 15s) for streaming `/v1/responses` and `/v1/chat/completions`
  while waiting for upstream headers (response committed as 200 SSE after 15s; later failures arrive as an
  in-stream `response.failed` / chat error event) and during upstream silence (only at event boundaries).

### Ops
- Production Build `responseHeaderTimeout` lowered from 30 min to 5 min (code default already 5 min).

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
