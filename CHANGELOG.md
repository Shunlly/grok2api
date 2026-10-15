# Changelog

Shunlly/grok2api fork of [chenyme/grok2api](https://github.com/chenyme/grok2api).

## v3.4.6 — 2026-10-15

### Added
- 账号列表和编辑框新增「额度刷新」提示：`上次同步 HH:mm · 下次刷新 HH:mm`；下次刷新取「下次定时同步」与「本账号重置后补同步」中较早的一个，后者标注「（重置后刷新）」。悬停说明定时间隔、重置后约 90 秒补同步、本账号重置后刷新时间，时间为 UTC+8。
- 自动同步关闭、账号停用、需重新登录或类型不支持时，直接显示对应原因而不是时间。
- 账号接口新增 `lastQuotaSyncAt`、`nextQuotaSyncAt`、`nextQuotaSyncReason`（hourly / reset / autoSyncDisabled / accountDisabled / reauthRequired / unsupported）和 `resetQuotaSyncAt`。

### Fixed
- 重置补同步时间按账号单独计算（调度器已登记的边界优先，未登记时用 `nextResetAt` + 90 秒），晚于下一次定时同步的也会显示，不再只在早于定时同步时才可见。

## v3.4.5 — 2026-10-15

### Added
- 后台定时同步额度：默认每 1 小时（`quota.autoSyncInterval`，`0` 关闭，范围 5m–24h；启动后 `autoSyncInitialDelay` 默认 2m 首次运行）同步全部启用且非 `reauthRequired` 的账号：Build Billing（含周额度周期）、Web 额度窗口和订阅、Console 额度，并顺带刷新 `account_usage_stats`。
- 周期重置补同步：每轮同步后记录每个账号已知的下一次重置时间（Billing `usage_period_end`、额度窗口 `reset_at` 取最早），在重置后约 90 秒只对该账号额外同步一次，让周期统计及时归零。
- 防突发：并发默认 2（`quota.autoSyncConcurrency`，1–8），每个账号请求前随机等待 0–5 秒，仍走共享同步池。失败只记 `quota_auto_sync_account_failed` 日志，账号健康状态只由原有同步逻辑决定。
- `GET /api/admin/v1/accounts/auto-sync/status`；账号页标题旁显示「自动同步 · 下次时间」，悬停显示间隔、上次运行结果、下次定时和重置补同步时间（UTC+8）。
- 该配置目前只在 `config.yaml` 中设置，未接入运行时设置页。

## v3.4.4 — 2026-10-15

### Changed
- 账号用量统计改为只统计**当前额度周期**：「总请求 / 总Token / 总金额」改名为「周期请求 / 周期Token / 周期金额」，每次同步额度时按 `[周期开始, 现在]` 从 `request_audits` 重算（排除规则不变：探针、定时考题、内部 Key），周期滚动后自动归零。「今日金额」保持不变。
- 周期来源优先级：Build 用上游 Billing `usage_period_start..usage_period_end`；Web/Console 用额度窗口 `reset_at − window_seconds .. reset_at`（多模式取窗口最长的）；都拿不到时回退为滚动 7 天。快照过期时按周期长度自动向前滚动。
- 悬停提示显示周期起止（UTC+8）和来源。API `usageStats` 新增 `periodStart` / `periodEnd` / `periodSource`，`total*` 字段语义改为周期内用量。
- 去掉 v3.4.2 的增量累计逻辑；`account_usage_stats` 旧列保留以兼容，`first_audit_at` 不再更新。

## v3.4.3 — 2026-10-15

### Changed
- 去掉账号列表和编辑弹窗里的「凭证过期 (Access Token)」显示：Access Token 约 6 小时自动续期一次，平时显示没有意义。API 字段（`expiresAt` / `expiresSource` / `expiresAutoRenew`）保留不变，兼容旧调用方。

### Added
- 「凭证需重新登录」琥珀色提醒：只在 OAuth（Build）凭证无法自动续期时出现，包括没有 refresh token、续期失败次数 > 0、Access Token 已过期超过 10 分钟仍未续上。悬停显示原因、续期失败次数、最近错误、上次续期时间和 Access Token 过期时间（UTC+8）。
- 账号已是 `reauthRequired` 时，状态列已有「需重新登录」徽章，不再重复显示这个提醒。SSO（Web）账号没有 refresh token，不会因此误报。

## v3.4.2 — 2026-10-15

### Added
- 账号用量统计：账号列表每行新增「总请求 / 总Token / 总金额 / 今日金额」四个标签，编辑弹窗新增「用量统计」。悬停显示明细：累计与今日的请求数（含失败数）、输入 / 缓存 / 输出 / 推理 Token、金额拆分（上游计费 + 本地估算）、统计起点和统计时间（UTC+8）。
- 数据来自 `request_audits` 的真实请求，规则与 v3.3.8「最近使用」一致：排除 `quality_` 前缀的探针 / 定时考题，以及内部 Client Key 的请求。失败请求计入次数，不产生 Token 和金额。金额 = 上游返回的 `cost_in_usd_ticks`，缺失时用本地价格估算，是按 API 价格折算的等价费用，不是订阅实际扣费。
- 快照存在新表 `account_usage_stats`，只在同步额度时刷新：单账号「同步额度」、全部同步、批量同步、自动恢复复检，以及首次导入同步时都会刷新，刷新失败不影响额度同步。读取账号列表时不重算。
- 累计值按审计 ID 水位增量累加，所以审计到了 30 天保留期被清理后，总数也不会回落。首次统计只能覆盖还在保留期内的记录。今日值在每次刷新时按北京时间自然日重算；跨日后还没重新同步时，今日金额显示 $0.00，悬停会提示。
- 账号 API 新增 `usageStats`，金额单位为 USD ticks（1 USD = 1e10）。

## v3.4.1 — 2026-10-15

### Fixed
- 账号时间显示时区错误：之前按浏览器本地时区格式化，浏览器/系统时区为美西（PT）时，access token 08:58Z 显示成「10-15 01:58 · 4小时后」（UTC+8 实际是 16:58）。日期本身和倒计时都基于同一时间戳，只是钟点按 PT 渲染。现在控制台统一按显示时区格式化（默认 `Asia/Shanghai`，可用 localStorage `grok2api.timeZone` 覆盖），24 小时制，悬停显示带「UTC+8」标签的完整时间；仪表盘统计也改用同一时区。

### Changed
- 「过期时间」更名为「凭证过期 (Access Token)」，悬停说明这是 OAuth Access Token（约 6 小时一换，refresh token 自动续期），不是套餐到期。

### Added
- 新增「套餐到期」：账号列表与编辑弹窗显示 SuperGrok / Heavy 订阅当前计费周期结束时间。数据来自 Grok Web `GET /rest/subscriptions`（`billingPeriodEnd`、`status`、`cancelAtPeriodEnd`、`dominantPlan.plan`），在 Web 账号「同步额度」时顺带刷新（失败不影响额度同步），存入新表 `account_plan_subscriptions`。Build 接口（`/user?include=subscription`、`/billing`）只返回套餐等级和周额度周期，没有到期日，因此 Build 账号取关联 Web 账号的订阅快照；Console 是预付费额度，没有套餐到期，显示 -。3 天内到期（未自动续订）标黄，已过期标红；`cancelAtPeriodEnd=true` 显示「已取消自动续订」。周额度周期不会被当作套餐到期。
- 账号 API 新增 `planExpiresAt`、`planExpiresSource`（`webSubscriptions` / `linkedWebSubscriptions`）、`planAutoRenew`、`planCancelAtPeriodEnd`、`planStatus`、`planName`、`planBillingInterval`、`planSyncedAt`、`planNoSubscription`。

## v3.4.0 — 2026-10-15

### Added
- 账号操作菜单新增「查看模型」：弹窗分「可用模型 / 不可用 / 全部模型」三个标签。可用模型 = 该账号 Provider 下当前能路由到这个账号的公开模型；不可用列出原因（模型已停用、账号上游不支持、模型绑定了其他账号、单模型额度封锁（含解除时间）、账号禁用/凭证失效/冷却/无额度/额度待重置/检测中）。全部模型 = 上游为该账号返回并已同步的模型能力列表（Build 显示有效上下文），附对应的公开模型。支持「从上游刷新」（复用现有账号模型同步）。
- 新接口 `GET /api/admin/v1/accounts/:id/models`、`POST /api/admin/v1/accounts/:id/models/refresh`，返回 `usable`、`unavailable`、`all`、`accountBlockers`、`capabilityKnown`、`fetchedAt`、`lastError`、`source`。

### Fixed
- 部署后旧页面加载已失效的动态模块（`Failed to fetch dynamically imported module`）时自动刷新一次（`vite:preloadError` + 路由 errorElement，sessionStorage 30 秒防循环），刷新失败显示友好提示与「刷新页面」按钮。`index.html` 继续 `Cache-Control: no-cache`，`/assets/*` 为 `public, max-age=31536000, immutable`。

## v3.3.9 — 2026-10-15

### Changed
- Console 额度上限为 0（上游 `/v1/usage` 的 chat 窗口 `limit=0`，未授予/未充值）的账号显示「无额度」，不再显示「待重置」；不计入「额度待重置」卡片，改为卡片内单独的「无额度 N」；不再为 `limit=0` 窗口合成「当前 + 24 小时」的重置时间，下次重置显示 -。调度排除逻辑不变。
- 账号汇总 API `recovery` 新增 `noQuota`，状态筛选新增 `noQuota`；账号 API 新增 `noQuota`。
- Build 账号类型按上游套餐名显示：SuperGrokPro / SuperGrok Heavy → Heavy，SuperGrok → SuperGrok，SuperGrok Lite → Lite，Free → Free，其他显示原值；悬停显示原始套餐名。账号 API 新增 `planTier`、`planRaw`。付费/免费调度逻辑不变。

## v3.3.8 — 2026-10-15

### Added
- 账号列表与编辑弹窗显示「过期时间」和「最近使用」（浏览器本地时区 + 相对时间，未知显示 -，悬停显示完整时间与来源）。
- 账号 API 新增 `expiresSource`（`oauthAccessToken` / `credential`）、`expiresAutoRenew`、`lastUsedSource`（`requestAudit` / `accountColumn`）；`lastUsedAt` 改为真实请求时间。
- 过期时间规则：Build OAuth 为 access token 过期时间（约 6 小时）；有 refresh token 且续期未失败时标记自动续期、不高亮，否则已过期标红、3 天内到期标黄。Web / Console SSO 上游不提供 Cookie 过期时间，显示 -。上游未提供 SuperGrok 订阅到期日（Billing `billing_period_end` 等于周额度窗口），不作为过期时间。
- 最近使用规则：`request_audits` 中该账号最近一次 `status_code < 400` 的请求，排除 `quality_` 前缀（出口探针、定时考题）与内部 Client Key；每个账号走 `idx_audits_account_created_id` 倒序取 1 条。无审计记录（如超过保留期）时回退到账号表 `last_used_at`（可能含探针）。

## v3.3.7 — 2026-10-15

### Changed
- 仪表盘首字统计排除出口质量探针与定时考题（`request_id` 前缀 `quality_` 或内部 Client Key 发出的请求）；主值改为中位数，附均值、p90 与样本数。
- 新增按输入长度分组（<2万、2万–6万、6万–12万、12万–20万、>20万）的样本数与中位数，悬停查看；提示说明首字包含上游 prefill，随上下文增长。
- Dashboard API `usage` 新增 `medianFirstTokenMs`、`p90FirstTokenMs`、`firstTokenBuckets`。

### Added
- 账号列表与编辑弹窗显示「下次重置」（浏览器本地时区 + 倒计时，未知显示 -）。账号 API 新增 `nextResetAt` / `nextResetSource`。
  取值：额度耗尽待重置时优先 Billing 周期结束（`quota.periodEnd`），否则 `account_quota_recovery.next_probe_at`；其余取最早的未来时间：Build Billing `usage_period_end`、Web/Console `account_quota_windows.reset_at`（窗口秒数 > 0）、`account_model_quota_blocks.cooldown_until`。已过期的时间忽略。

## v3.3.4 — 2026-10-14

### Added
- 金丝雀探测保存产物：新表 `canary_probe_artifacts` 记录题目、模型回答（上限 200KB）、思考文本（上限 64KB）、输入/输出/思考/合计 token、响应模型、首字与总耗时、检查明细（期望答案与实际答案 / 检测到的 SVG 元素）。产物保留期与审计日志一致（`audit.retentionDays`），结果本身仍按 `canary.retentionDays` 保留。
- 管理接口 `GET /api/admin/v1/quality/canary/results/:id` 返回单条结果及产物（旧结果 `artifact` 为 null）。
- 面板：点击账号行展开最新结果，点击历史小方块查看对应那次；显示题目、回答、思考过程、用量、耗时和判定原因。SVG/HTML 回答在空 sandbox 的 iframe 中渲染（禁用脚本、外部资源由 CSP 阻止，并剥离 script/事件属性），可切换原文。旧结果显示“没有记录产物”。

## v3.3.3 — 2026-10-14

### Added
- Build 账号编辑弹窗新增「上下文窗口」设置：选项来自上游目录的可选大小（如 256k / 500k），默认「自动」取最大。按账号存储（`provider_accounts.context_window`，0 = 自动，现有账号默认自动）。
- `/v1/models` 不区分账号，因此 Codex 目录与上下文预检按所有启用 Build 账号中最大的设置生效；任一账号为自动则不限制。自动压缩阈值按生效窗口计算，仍封顶 300k。
- 只读管理接口 `GET /api/admin/v1/accounts/context-windows`：返回缓存的上游目录上下文字段（`context_window`、`contextWindows`、可选大小、生效值、自动压缩百分比）。

### Fixed
- v3.3.2 的上下文窗口测试文件名以 `_windows_test.go` 结尾，被 Go 当作仅 Windows 构建而未执行；已改名，测试现在会实际运行。

## v3.3.2 — 2026-10-14

### Fixed
- Build 模型上下文按上游目录的可选窗口取最大值：解析 `contextWindows` / `context_windows` / `_meta.contextWindows`（如 `[256000, 500000]`），`context_window` 只是默认选项。Codex 模型列表与上下文预检改用最大可选窗口，grok-4.5/4.6/4.7/4.7-build-fast 恢复为 500k，自动压缩阈值仍封顶 300k。
- 内置表补上 `grok-4.7-build-fast`（500k），未同步目录时不再回落到 128k。

## v3.3.1 — 2026-10-14

### Fixed
- 「立即重检」接口返回的 `accountId` 改为字符串（与其他 ID 一致），修复面板解析重检结果失败的问题。

## v3.3.0 — 2026-10-14

### Added
- **账号自动恢复（自动重新激活）**：新后台任务每 10 分钟检查一次，只处理被判定为「临时失效」的 `reauthRequired` 账号（如上游偶发 `401 Authentication required`、凭据被拒）。Build 账号先强制刷新 OAuth token，再做一次只读的账单/额度请求；通过即恢复为 `active`。
  - 失败退避 10m → 30m → 2h → 6h → 24h（带随机抖动），一次只探测一个账号，约 5 天（`maxAge`）或遇到封禁 / 策略 403 / 永久 refresh 错误码后转为「永久」并停止。
  - **永不触碰**：管理员手动停用的账号、永久失效账号、`refresh_permanent` 凭据。被吊销或封禁的凭据无法被正当恢复，需要重新登录或导入。
  - 质量检测自动停用（`auto_quality`）的账号默认不处理；开启 `autoReactivate.includeQualityDisabled` 后，会在冷却保护下用金丝雀考题单独探测，通过才重新启用。
- **失效原因记录**：账号新增 `disabled_source`（admin / auto_quality / auto_auth）、`disabled_reason`、`reauth_kind`（transient / permanent）以及重检次数、下次/上次重检时间、最近结果字段（启动时自动迁移；旧的停用账号视为管理员停用）。
- **面板**：状态列悬停显示停用来源、原因、失效类型、重检次数、下次重检时间和最近结果；行菜单新增「立即重检」（`POST /api/admin/v1/accounts/:id/recheck`）。对正常账号只做只读校验，不改任何状态。
- **状态变更日志**：`account_state_changed`（from / to / reason / reauth_kind）、`account_recovery_probe_failed`、`account_recovery_quality_failed`。
- 配置：`config.yaml` 顶层 `autoReactivate.enabled`（默认 true）、`interval`、`maxAge`、`includeQualityDisabled`（默认 false）。

## v3.2.3 — 2026-10-13

### Changed
- **移除「降智账号」面板**：质量守护页删除该标签页，后端 `GET /request-audits/degrade-accounts` 路由下线。
- **彻底停用按速度（Token/s）的降智判定**：网关不再在请求结束时计算 burst/soft/hard 分类（原仅观测日志 `quality_fast_output_observed` 也一并移除）。
- 保留 `missing_thinking`/`quality_degraded` 检测与定时考题（6 小时、仅观测）。

### Added
- **账号列表质量提示**：仅当账号近 7 天有真实 `quality_degraded` 命中或定时考题未通过时，在名称旁显示小警告图标，悬停可见次数、最近时间（本地时区）与原因；无命中不显示。新接口 `GET /api/admin/v1/quality/account-flags`。

## v3.2.2 — 2026-10-13

### Fixed
- **降智面板误报**：「降智账号」列表与「仍启用」计数此前会把仅触发速度类分类（burst/soft/hard，PR #7 已降为仅观测）的账号列出来，导致健康账号常驻面板。现在默认只列出真实 `missing_thinking` 命中的账号；按速度类筛选时仍可查看观测数据。
- 降智趋势图时间标签改为按服务器本地时区（容器 `TZ=Asia/Shanghai`）显示，不再显示 UTC。

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
