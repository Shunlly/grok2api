# Changelog

Shunlly/grok2api fork of [chenyme/grok2api](https://github.com/chenyme/grok2api).

## v3.5.7 — 2026-10-20

### Changed
- 默认 Grok Build 客户端版本 1.0.60 → 1.0.62（User-Agent `grok-shell/1.0.62 (linux; x86_64)`，npm `@xai-official/grok` 当前 latest）。上线前已在生产通过设置热更新实测 grok-4.7 / grok-4.5 正常。

## v3.5.6 — 2026-10-19

### Changed
- 默认 Grok Build 客户端版本 1.0.50 → 1.0.60（User-Agent `grok-shell/1.0.60 (linux; x86_64)`，同步上游 chenyme 4e220e81）。
- 同步上游 4e220e81：换号重发同一轮时向 Build 上游带 `x-grok-transient-retry: N`（首次提交不带）。只有传输错误 / 5xx 计数，质量重试（缺 thinking、空流）、输出循环、空响应不计；Cloudflare 525/526 源站 TLS 错误不再换号重试。

## v3.5.5 — 2026-10-17

### Changed
- 同步上游 dbd1a29f（voice-ws 区分取消与超时）、659ee8ca（autoDisable 运行时启用请求内检测；上下文压缩请求豁免账号质量惩罚）；autoDisable 默认关闭。
- 模型页绑定账号支持「全选 / 清空」（按当前搜索结果），适配手机。

## v3.5.4 — 2026-10-17

### Changed（手机适配，桌面布局不变）
- 全局：手机上主内容边距收紧（px-4 / py-5），页脚在手机上改为文档流内显示，不再浮在内容上；大弹窗基础样式增加 `max-h: 100dvh-1rem` 与内部滚动。
- 仪表盘：统计卡片手机上两列紧凑显示；修复「Top 模型」表把页面撑出 222px 横向滚动（网格子项 min-w-0，表格在卡片内横滑）；趋势图 token 轴加宽，不再截断「8000万」。
- 上游账号：统计卡片两列；Build / Web / Console 切换在手机上省略「Grok」前缀不溢出；账号列表在 < 768px 时改为卡片（名称 + 徽标、类型、状态、凭证续期、额度 / 下次重置 / 套餐到期 / 最近使用 / 同步提示 / 用量统计、创建时间、操作菜单），桌面仍为表格。
- 请求审计：统计卡片两列；列表在手机上改为卡片（模型 / 账号、状态（点开详情）、计费、Token、耗时、出口、时间）。
- 弹窗：账号编辑、查看模型、请求诊断详情、密钥、模型路由、出口节点、质量守护、视频详情等大弹窗在手机上全屏（100dvh，顶部对齐，适配安全区）。

## v3.5.3 — 2026-10-15

### Fixed
- 自动压缩只按 token 触发，Codex 会话里大量字节来自加密推理（`encrypted_content`）/ 最近截图等「字节多、token 估算少」的内容：v3.5.2 把 4.0 MB 请求压到约 10 万 token，但仍有 3.4 MB，结果 413，连 Codex 的 /compact 也 413。
- 现在请求体超过 `maxUpstreamRequestBytes` 时也会触发压缩（`byte_pressure`），保留的最近部分同时受字节预算约束（≤ 限额 85% 的一半），已缓存摘要不满足字节目标时会增量重新摘要；仍超出时作为最后手段，去掉最后一条用户消息之前的图片 / 多模态和推理（reasoning）项。只有这样仍放不下才返回 413。
- token 估算不再把长 base64 样式字符串（加密推理）当作图片，按实际长度计。
- Codex `/compact`（compaction_trigger）、Grok TUI 压缩请求和 `/v1/responses/compact` 超过大小限制时也会先压缩再转发，不再直接 413（未超限时保持原样）。
- `context_compacted` 日志新增 `byte_pressure`、`bytes_reduced`、`operation`。

## v3.5.2 — 2026-10-15

### Added
- 自动上下文压缩（模仿 Grok Build `auto_compact_threshold_percent=85`）：估算输入 token 超过模型上下文窗口 85% 时，用同一模型（同一 Client Key / 账号池）按 Grok Build 的 full-replace 摘要提示词把较早历史写成摘要，替换为「This session is being continued…」开头的用户消息；保留 system/developer、最后一条用户消息、最近 2 轮（工具调用与输出成对保留）。摘要上限 60k 字符、预留 32768 输出 token。摘要按前缀哈希链缓存（内存 LRU 256），Codex 重发全量历史时复用、只增量补摘要；摘要失败退化为丢弃最早记录 + 占位，不报错。配置 `gateway.autoCompact.*`，日志 `context_compacted`，审计请求头 `X-Grok2api-Context-Compacted`（前后 token / 字节）。摘要请求本身以 `compaction` 操作计入审计。
- 工具输出源头截断（Grok Build 限制）：所有轮次的工具输出——普通 40000 字节、终端类 20000 字符，保留头 60% + 尾 40%。配置 `gateway.requestSlimming.toolOutputMaxBytes / shellOutputMaxChars`。

### Changed
- `gateway.maxUpstreamRequestBytes` 默认降为 2.5 MiB（在压缩之后检查）。
- ≥2 MiB 的大请求遇到慢速 401 或 Cloudflare 5xx 时立即失败，返回 413 `request_too_large` 和明确提示（不再显示「上游服务暂不可用」，也不再换号重试）。
- 账号列表的周期 / 今日用量改为打开列表时按本周期实时重算（只读），不再显示整点同步时的旧快照；请求数显示「成功 · 失败」分开计数。

## v3.5.1 — 2026-10-15

### Fixed
- 账号行「定时考题未通过」提示不再永久保留：现在只统计账号最近一次考题通过之后的连续失败（fail / error），之后只要有一次考题通过，提示即自动消失。悬停文案改为「定时考题自上次通过后连续未通过 N 次」。

## v3.5.0 — 2026-10-15

### Added
- 账号级模型允许列表：账号编辑页新增「可调用模型」，可开启「仅允许选定模型」并勾选上游模型（支持搜索、全选 / 全不选，列表来自 `/api/admin/v1/accounts/:id/models` 的上游模型并标注对应公开模型）。未勾选 / 关闭 = 允许全部模型（默认，向后兼容）。
- 持久化：`provider_accounts.allowed_models`（JSON 数组，默认空字符串），启动时 AutoMigrate 自动加列；账号 API 返回 `allowedModels`，`PATCH /accounts/:id` 接受 `allowedModels`（`[]` 清空恢复全部允许）。
- 调度强制：选号时（含 sticky 会话、previous_response 绑定账号的推理请求）账号仅在请求模型（公开模型映射后的上游模型）位于其允许列表内时才会被选中；全部账号都排除该模型时返回 503 `upstream_model_unavailable`「当前账号池没有支持该模型的可用账号（账号不支持或未在账号模型允许列表中）」。
- 「查看模型」中被排除的模型显示在不可用里，原因「未在账号允许列表」（`notAllowed`）；账号行显示「限 N 个模型」徽标，悬停列出允许的模型。

### Changed
- 账号编辑弹窗自适应：最大高度 90dvh、内容区内部滚动、标题与保存 / 取消按钮固定；宽度 `max-w-3xl`，小屏接近全宽；按「基础设置 / 状态与用量 / 可调用模型 / 高级设置（可折叠）」分组。「查看模型」弹窗同样限制在 90dvh 内滚动。

### Notes
- 允许列表按上游模型匹配：映射到同一上游模型的多个公开模型一起允许 / 排除。
- 管理员手动对指定账号发起的质量探测 / 额度探测、模型同步不受允许列表限制（它们是针对账号本身的诊断）。

## v3.4.10 — 2026-10-15

### Added
- 请求瘦身扩展到所有多模态 / 二进制内容：早于最近 `keepMediaTurns`（默认 2）轮用户对话的视频（`input_video`、`video_url`、`video`）、音频（`input_audio`、`audio_url`、`audio`）、内嵌文件 / 文档（Responses `input_file` 带 `file_data`、Chat `file` 带 `file_data`、Anthropic `document` 含 base64 PDF / 文本源）以及任意字符串里 ≥1KB 的 `data:*;base64,...` 内嵌数据（含工具输出），分别替换为「[视频已省略]」「[音频已省略]」「[文件已省略: 文件名]」「[内嵌数据已省略: MIME]」。
- 仅以 `file_id` / URL 引用的文件（无内嵌数据）保持不变；最近轮次内容（含 URL 引用的媒体）不改动。占位文本类型随格式：Responses 用 `input_text`，Chat / Anthropic 用 `text`。
- 日志 `request_slimmed` 与审计头 `X-Grok2api-Request-Slimming` 按类型记录：`images_removed`、`videos_removed`、`audios_removed`、`files_removed`、`data_uris_removed`，以及合计 `media_removed`。

### Changed
- 配置项改名为 `gateway.requestSlimming.keepMediaTurns`；旧的 `keepImageTurns` 仍兼容（`keepMediaTurns` 未设置时生效）。

## v3.4.9 — 2026-10-15

### Added
- 请求瘦身 `gateway.requestSlimming`（默认开启）：生成类请求转发给 xAI 之前，只保留最近 `keepImageTurns`（默认 2）轮用户对话里的图片，更早的图片（Responses `input_image`、Chat `image_url`、Anthropic `image`，含工具结果里的图片）替换为文字「[图片已省略 / image omitted to save context]」；早于最近 `keepToolOutputTurns`（默认 2）轮、超过 16KB 的工具输出只保留开头 6KB + 结尾 4KB，中间标注「[中间 N 字节已省略]」。最新一轮永不改动。
- 瘦身在 3 MiB 请求体保护之前执行，保护检查的是瘦身后的大小；条目顺序、call_id 配对、加密 reasoning 条目保持不变；输出是请求体的确定性函数，同一历史前缀在多次请求间保持一致。
- 日志 `request_slimmed` 记录原始/瘦身后字节、移除图片数、截断输出数；审计请求头中附加 `X-Grok2api-Request-Slimming`。
- Build 会话身份（soft session）仍按原始请求体计算，瘦身不会切换会话。

### Notes
- 暂不对保留下来的图片做缩放。

## v3.4.8 — 2026-10-15

### Added
- 生成类请求（/v1/responses、/v1/chat/completions、/v1/messages、/responses/compact）新增请求体大小保护 `gateway.maxUpstreamRequestBytes`，默认 3 MiB（3145728），0 关闭。超过时在调用 xAI 之前直接返回 HTTP 413 `request_too_large`，提示「请求过大（X MB > 3 MB），xAI 上游无法处理，请在 Codex 中执行 /compact 或新开会话 / Request too large ...」；不占用账号、不重试、不切换账号，审计记录错误码 `request_too_large`。
- 配置校验：`gateway.maxUpstreamRequestBytes` 不能为负数，且不能大于 `server.maxBodyBytes`（默认 32 MiB），保证超限请求能被完整读入并得到干净的 413。

### Fixed
- 大请求（≥ 2 MiB）遇到 Cloudflare HTML 5xx 时不再给账号加冷却。
- 大请求在上游耗时 ≥ 30 秒后才返回 401 时，视为请求级失败：不标记需重新登录、不刷新凭据、不切换账号。

## v3.4.7 — 2026-10-15

### Changed
- 账号「今日」用量（今日请求 / Token / 金额）改为只统计本周期内的部分：起算点 = max(北京时间当日零点, 周期开始)，今日数值不再超过周期数值。
- 周期在今天重置时，悬停提示显示「今日从 HH:mm（周期重置）起算」。
- 用量快照新增 `todayStart` 字段（接口 `usageStats.todayStart`）。

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
