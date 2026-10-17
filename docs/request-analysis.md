# 逐次用量与首字分析

历史分析包含“任务分析”和“逐次调用”。支持 Desktop、Codex CLI、VS Code Codex 插件、MonoCode 和已配置的 Pi 来源，列表不显示来源列，也不需要 API Key。

VS Code 插件会话读取同一 `CODEX_HOME` 下的 `sessions/` 与 `archived_sessions/`，保留 `codex_vscode` 原始标识，CSV 的 `source` 为 `vscode`。升级后自动重扫以前忽略的历史日志，仍按响应 ID 去重；旧累计记录按会话隔离差分，尊重清空历史边界。已知 Desktop / CLI / MonoCode 身份优先于 `source=vscode`，避免将共享 app-server 的其他客户端误分类。远程 SSH、容器或 WSL 的日志需要 Monitor 能访问其 Codex 数据目录，本机目录不会包含远程日志。

恢复同一会话时追加的 `session_meta` 保留已有模型、轮次及累计状态，切换会话才清空；模型也会读取明确的 `world_state.state.model` 与 `thread_settings_applied.thread_settings.model`。解析版本 4 自动重扫旧日志，只补回已有用量的缺失模型，不覆盖已知模型或重复累加 Token。

## 查看记录

在“逐次调用”中，每条用量记录显示时间、状态、模型、首字时间、输入 / 输出 Token、缓存命中和 API 等值估算。可以按响应 / 记录 ID、任务、会话、项目或模型搜索，按用量 / 失败 / 重试筛选，并按时间、Token、费用或首字时间排序。每页 50 条；列表内部可滚动，点击或按 Enter 展开详情。当前筛选可导出 CSV，包含 TTFT、HTTP 状态、错误类别和重试序号。

任务运行记录展开后增加“任务内调用分析”，按时间展示该轮次全部关联记录，每页 25 条。显示用量条数、失败 / 重试事件数、首字样本数、平均和 P95 首字时间；展开单条可查看任务 Token 占比及已定价费用占比。任务详情保留完整轮次，遵循账号筛选；调用列表及导出遵循时间、账号、模型、项目和调用筛选。顶部全局汇总仍遵循全局筛选。

缓存输入已包含在输入 Token 中，推理 Token 已包含在输出 Token 中，不能重复相加。费用按原有价格生效时间计算，属于 API 等值估算，并非官方订阅实付账单。未定价、未采集 TTFT 或没有独立用量的失败事件保持未知。

新版 Codex 的 `token_usage_record` 按响应 ID 去重；旧日志显示累计差分记录，不能保证每条差分对应一个完整请求。Pi 延续原有会话及响应去重规则，目前每个 assistant 用量消息对应一个任务轮次。

## 开启首字时间（TTFT）

首字时间是单次模型请求发出至首次模型内容输出的等待时间，包含首次推理 / 文本 / 工具流内容，不是整个任务耗时。Codex 使用其完成事件中明确的 `ttft_ms`；Pi 扩展使用 provider 请求前与首次非空内容增量的单调时钟，兼容复用 WebSocket 的请求。平均 / P95 使用已关联用量的 TTFT 样本；失败后仍有明确首字时间的诊断行也会显示该值。没有数据时不会用相邻日志时间差补算，也无法补回未采集的历史 TTFT。

新安装默认开启本机首字采集；升级保留已有开关。打开 Monitor 或在“设置与价格 → 通用 → 本机首字采集”开启开关后，程序自动完成客户端接入，无需复制配置、编辑文件或手动加载扩展。

- **Codex Desktop / CLI / VS Code 插件**：在 Monitor 当前使用的 `CODEX_HOME/config.toml`（默认 `%USERPROFILE%\.codex\config.toml`）自动合并本机 OTel 导出器，并关闭提示词导出。模型、登录、其他设置和遥测字段保留；第一次修改前在同目录保留 `config.toml.codex-monitor.bak`。客户端共用该目录时同时生效，使用其他目录时按 Monitor 的 `--home` / `MONITOR_CODEX_HOME` / `CODEX_HOME` 配置接入。
- **Pi**：将内置扩展自动安装到 `PI_CODING_AGENT_DIR/extensions/codex-monitor.ts`（默认 `%USERPROFILE%\.pi\agent\extensions\codex-monitor.ts`）。正常启动 `pi` 即自动加载，无需 `-e`。Pi 目录遵循 `MONITOR_PI_HOME` / `PI_CODING_AGENT_DIR`。
- **已有运行中的客户端**：配置只在客户端初始化时读取，因此首次接入或修改端口后需重启 Codex Desktop / CLI 一次，VS Code 执行“Developer: Reload Window”；Pi 可重启或执行 `/reload`。之后的新调用自动采集，历史缺失时间无法补算。

接收器默认监听 `127.0.0.1:4319`。启动时端口被占用会自动选择后续 16 个端口内的空闲端口，并同步客户端配置；也可在设置中应用其他端口。手动选择被占用的端口会回滚设置。关闭开关停止接收，并撤回程序管理的 Codex 字段及 Pi 扩展；用户在此期间修改的其他配置仍保留。完全退出 Monitor 时接收器停止，客户端配置保留，便于下次启动继续采集。

设置显示每个客户端的自动配置结果：“接收就绪，等待新调用”表示接收器已启动；“已收到首字数据”表示确实收到完成事件中的 TTFT。“最近接收”显示事件到达时间。用量及日志中的重试事件可独立采集到，看到这些记录不表示首字已经接通。“重新检查自动配置”可重新检查和同步，无需手动复制。

已有其他 OTel 导出器、不同的同名 Pi 扩展、无效或不可写配置不会被静默覆盖，设置中显示具体原因。关闭时仅恢复仍与程序管理状态相符的字段，不覆盖之后的手动更改。离线模式不启动接收器，也不修改客户端文件。

Codex 使用官方 OTel 日志导出，对官方订阅和 CLI 同样适用。自动配置仅设置 `log_user_prompt = false` 及指向本机接收器的 OTLP HTTP JSON exporter；支持 gzip，不提供 gRPC / protobuf 接口。Codex 异步批量导出，事件可能晚于会话日志到达，界面会在收到并关联后更新。配置依据：[官方 Codex 可观测性文档](https://learn.chatgpt.com/docs/config-file/config-advanced#observability-and-telemetry)。

Pi 扩展只发送时间、Token 签名、响应 ID、模型和诊断类别，不更改模型请求、正文或认证配置。延续原有官方登录识别规则：`openai-codex`，或能通过当前本机 OAuth 配置识别的 `openai`；不扩展到未知 API 登录来源。Monitor 接收失败不改变模型结果，未启动时诊断不保存到磁盘。需要支持 provider / message hooks，已在 `@earendil-works/pi-coding-agent` 1.1.0 验证加载。没有请求开始或首内容事件的记录保持未知。

如独立手动加载仓库中的 `integrations/pi/codex-monitor.ts`，可设置 `CODEX_MONITOR_OTEL_URL=http://127.0.0.1:<端口>/v1/logs`；扩展只接受 `127.0.0.1` 上的 HTTP 目标。已有该环境变量时需与 Monitor 端口一致。自动加载后不要再用 `-e` 加载另一份相同扩展。

## 失败、重试与关联口径

- Codex 用量扫描同时只读读取 `CODEX_HOME` 下最高版本的 `logs_*.sqlite`，提取客户端明确记录的 sampling retry。诊断数据库缺失或格式暂不支持，不影响原有用量采集。
- OTel 的 API / SSE / WebSocket 错误记录补充失败事件、可用的 HTTP 状态和请求尝试序号。HTTP 成功的重试尝试也可记录为重试事件。流重试和 HTTP 重试属于不同层级，事件数不等于所有网络请求的精确重试次数。
- Pi 扩展记录 provider 可见的失败及重复请求尝试；显式请求关联键将重试和最终用量归入同一个 Pi 响应轮次。Pi 在钩子以下完成、未暴露的内部重试无法统计。
- TTFT 优先按响应 ID 关联。当前 Codex OTel 缺少响应 ID 时，只关联同会话、Token 签名且相差不超过 2 秒的唯一用量候选；已知模型必须一致，缺失模型允许使用已采集的明确模型补齐，界面标注“按时间与 Token 唯一匹配”。多个候选时保留未关联数据，不猜测。
- Codex 错误事件有轮次 ID 时直接归属；缺少轮次 ID 时，仅在该事件时间对应一个唯一任务区间时归属。无法确定的事件仍可在逐次列表中查看。
- 没有 `usage` 的失败 / 重试事件不贡献 Token 或费用；部分失败可能另有带用量的记录。因此此处展示观测到的事件，不计算未经完整采集证明的请求成功率。

## 数据与验证

数据库结构版本 3 新增 `request_events` 及索引，版本 4 扩展会话来源以保存 `vscode`；保留原始会话、现有账号、价格、设置或清空历史边界。清空历史同时清除请求事件，并阻止清空时间以前的 OTel 批次重新写入。数据库备份修复保留新增表和结构版本。

接收器只监听 `127.0.0.1`，随采集开关及服务生命周期启停；离线模式不监听。请求有大小和超时限制。接收时按字段白名单丢弃正文、工具内容、账户身份及 headers；错误原文转换为限流、超时、连接、认证、服务等固定类别后保存，无外部转发。

```powershell
go test ./...
npm run test:pi
npm run native:check
```

测试使用合成数据，覆盖关联歧义、晚到事件、去重、Pi 响应键与重试关联、账号筛选、任务占比、未知费用 / TTFT、分页、CSV、诊断游标重建、清空边界、HTTP 接收验证及界面键盘展开。Pi 扩展也通过了真实 Pi RPC 启动加载检查，该检查未发起模型请求。
