<a id="top"></a>

<div align="center">
  <img src="assets/monitor-glass.png" alt="Codex Monitor" width="104" />
  <h1>Codex Monitor</h1>
  <p><strong>本机 Codex 用量与额度，一眼看清。</strong></p>
  <p>Token 消耗 · 活动日历 · 任务记录 · 5h / 每周额度 · 桌面小窗口</p>
  <p>
    <a href="https://github.com/oysterhyd/codex-monitor/releases/latest"><img src="https://img.shields.io/github/v/release/oysterhyd/codex-monitor" alt="最新版本" /></a>
    <img src="https://img.shields.io/badge/Windows-x64-1671ff?logo=windows" alt="Windows x64" />
    <a href="https://mygo.egoist.dev/docs"><img src="https://img.shields.io/badge/UI-MyGO_native-008f80" alt="MyGO native UI" /></a>
    <img src="https://img.shields.io/badge/Data-Go_%2B_SQLite-007d9c?logo=go&logoColor=white" alt="Go and SQLite" />
    <img src="https://img.shields.io/badge/语言-简体中文_%2F_English-526783" alt="简体中文与 English" />
  </p>
  <p>
    <a href="https://github.com/oysterhyd/codex-monitor/releases/latest">下载</a> ·
    <a href="#功能">功能</a> ·
    <a href="#界面预览">界面预览</a> ·
    <a href="#快速开始">快速开始</a> ·
    <a href="#开发与构建">开发与构建</a> ·
    <a href="#指标口径">指标口径</a> ·
    <a href="#数据与隐私">数据与隐私</a> ·
    <a href="docs/architecture.md">架构说明</a>
  </p>
</div>

<p align="center">
  <img src="assets/readme/cover.png" alt="Codex Monitor 2.9.1 原生主窗口实际截图" width="1120" />
</p>

<p align="center"><sub>2.9.1 Windows 原生界面实机截图；全部使用合成演示数据。小窗口浅色 / 深色预览见下方。</sub></p>

<a id="功能"></a>

## 功能

Codex Monitor 是 Windows x64 本机监测工具。主窗口使用 **MyGO native UI**，透明桌面小窗口使用 **Go / Win32 原生透明窗口**；卡片、圆球、图表、动画和 SQLite 数据服务全部在同一个 Go 进程中运行。安装包包含完整应用，无需安装 Go、Node.js、Electron 或浏览器运行时。

| 页面 / 模式 | 能看到什么 |
| --- | --- |
| 用量总览 | Token、请求次数、缓存命中率、费用估算、使用趋势及模型 / 项目排行 |
| 活动日历 | 年度热力图、活跃天数、连续活动、小时分布与单日详情 |
| 历史分析 | 任务记录、逐次调用、任务内消耗占比、首字时间、失败重试事件、筛选分页与 CSV 导出 |
| 账户额度 | 当前 Codex 账号的 5h / 每周额度、重置时间和采样历史趋势 |
| 设置与价格 | 中英文、主题、提醒、开机启动、账号名称与价格版本 |
| 桌面小窗口 | 与主窗口同一套样式的卡片与 Token 圆球，跟随浅色 / 深色；拖动、置顶、用量趋势和位置恢复 |

支持今日、近 7 天、近 30 天、全部和自定义时间范围。范围切换复用有上限的统计缓存，数据变化后自动失效；趋势图切换保留原有展开动画与键盘焦点，复用后台图层减少绘制开销。刷新和导出按钮与末尾筛选框右对齐。窗口隐藏系统标题栏，侧栏使用 Windows Mica 材质，顶部玻璃工具栏随滚动出现；仍可拖动、缩放、最小化和最大化。关闭窗口后继续在托盘采集，完全退出请使用托盘菜单。

历史分析可切换“任务分析 / 逐次调用”。逐次记录采用紧凑行，显示时间与状态、模型、首字时间、输入 / 输出、缓存命中和费用；点击展开详情。任务内明细显示消耗占比、平均 / P95 首字和失败重试事件，不展示来源列。Desktop、CLI、VS Code Codex 插件和已配置的 Pi 用量均可采集。新安装默认开启首字采集，自动配置 Codex Desktop / CLI / VS Code 和 Pi；升级保留原开关，开启后自动接通。正在运行的 Desktop / CLI 只需重启一次，VS Code 重新加载窗口，Pi 也可执行 `/reload`；完整说明见 [逐次用量与首字分析](docs/request-analysis.md)。

2.9 按 Apple 的设计语言重做主窗口：系统色板的浅色 / 深色主题、彩色图标徽章、圆角卡片与分组设置。卡片按顺序以弹簧动画入场，侧栏选中胶囊随页面滑动，指标卡片悬停时轻微抬起，额度圆环从零转到当前值，数字以数值过渡变化。2.9.1 起小窗口使用同一套色板、字体、彩色徽章和圆角卡片，并跟随浅色 / 深色主题；GPT / Codex 等 OpenAI 模型显示 OpenAI 官方黑白标志。小窗口保留卡片 / 圆球动画与鼠标高光。两种界面均遵循系统减少动态效果设置。

Token、输出、USD 与额度历史采用平滑趋势曲线，保留真实峰谷、额度重置和采样空档。鼠标与键盘浏览的数值来自原始记录；密集额度采样只精简绘制点。动画复用后台绘制的曲线图层，长历史悬停使用二分查找。详见 [曲线平滑](docs/chart-smoothing.md) 与 [图表性能测量](docs/chart-performance.md)。

活动日历以波纹方式显现，悬停或用方向键选择日期时在下方显示当天详情，`Enter` 或点击打开当天记录。活跃时段以渐变柱条显示每小时的 Token 与记录数；最近活动日列出最近有记录的日期，点击行可打开当天记录。

| 快捷键 | 操作 |
| --- | --- |
| `Ctrl+K` | 打开命令面板 |
| `Alt+1`–`Alt+5` | 切换五个页面 |
| `Ctrl+R` | 刷新 |
| `Ctrl+E` | 导出当前筛选 |

<a id="界面预览"></a>

## 界面预览

下面是当前版本实际绘制的界面，均使用合成演示数据。

<p align="center">
  <img src="assets/readme/overview.png" alt="MyGO 原生主窗口：用量指标、额度圆环、趋势图和活动热力图" width="1120" />
</p>

<details>
<summary><strong>活动 · 历史分析（深色）</strong></summary>

<p align="center">
  <img src="assets/readme/activity.png" alt="活动页面：年度热力图、连续活动和小时分布" width="1120" />
</p>

<p align="center">
  <img src="assets/readme/history.png" alt="历史分析页面深色主题：指标卡片、消耗明细和运行指标" width="1120" />
</p>

</details>

<p align="center">
  <img src="assets/readme/widget.png" alt="桌面小窗口：5h 与周额度、实时 TPS、缓存命中率和今日 Token" width="560" />
</p>

<p align="center">
  <img src="assets/readme/widget-dark.png" alt="桌面小窗口深色主题：与主窗口统一的卡片、徽章与用量趋势" width="560" />
</p>

小窗口用量汇总本机全部账号，额度仅显示当前 Codex 账号。点击右上角三点收起为圆球，点击圆球展开；右键可设置置顶，双击标题或点击标志返回主窗口。

<a id="快速开始"></a>

## 快速开始

[下载 Windows x64 安装包](https://github.com/oysterhyd/codex-monitor/releases/download/v2.9.4/Codex-Monitor-Setup-2.9.4.exe) · [版本说明与 SHA-256 校验](https://github.com/oysterhyd/codex-monitor/releases/tag/v2.9.4)

运行 `Codex-Monitor-Setup-2.9.4.exe`，按向导选择安装目录。默认安装到 `%LOCALAPPDATA%\Programs\Codex Monitor`，无需管理员权限，并创建桌面和开始菜单快捷方式。

安装包内的原生主程序同时包含主窗口和桌面小窗口，使用时无需安装 Go 或 Node.js。实时数据需要本机已有 Codex 安装及登录状态。升级时会移除旧版小窗口已知的 Electron 运行文件，保留用户额外添加的文件。更新前请从托盘完全退出应用；安装和卸载均保留 `%APPDATA%\codex-monitor` 中的统计与设置。

从 2.6.1 起，**设置与价格 → 通用 → 软件更新 → 检查更新** 打开 MyGO 原生更新窗口，检查 GitHub 正式版本、显示版本说明并验证下载签名，安装后可重新启动。默认只手动检查；从 2.9.0 起可在同一分组开启每日自动检查和后台自动下载，下次启动时应用已验证的更新，选择会保留。更新不读取或上传监测记录。源码 `go run .` 的开发构建未启用签名更新，使用 `npm run build` 生成发布构建。内置更新替换主程序和图标；Windows 卸载登记的版本号在下一次运行安装包时同步。

<details>
<summary><strong>从源码运行或构建便携目录</strong></summary>

需要 **Windows x64 和 Go 1.27.1**。npm 包装脚本另需 Node.js 22.19+；项目没有 npm 依赖。实时数据需要本机已有 Codex 安装及登录状态。

```powershell
git clone https://github.com/oysterhyd/codex-monitor.git
cd codex-monitor
npm start
```

`npm start` 会构建并启动当前 native 版本。首次启动读取本机日志，随后增量更新；无需输入 API Key。

只构建运行目录：

```powershell
npm run build
```

产物位于 `build/native/`，包含 `Codex Monitor Native.exe` 和 `icon.ico`。**分发时复制整个目录**；主窗口与桌面小窗口均包含在可执行文件中，没有额外运行时目录。只使用 Go 也可运行 `go run .`，或通过 `go build -trimpath -ldflags="-s -w -H=windowsgui" -o "Codex Monitor Native.exe" .` 构建。

可将完整目录放到 `%LOCALAPPDATA%\Programs\Codex Monitor`，为 native 可执行文件创建桌面或开始菜单快捷方式。更新前先从托盘完全退出，用户数据保存在独立目录。

</details>

<a id="开发与构建"></a>

## 开发与构建

Go 可通过 PATH 查找，也支持 `%LOCALAPPDATA%\Programs\go\bin\go.exe` 或 `GO_EXE`。

| 命令 | 用途 |
| --- | --- |
| `npm start` | 构建并启动 native 应用 |
| `npm run build` | 构建完整 native 运行目录 |
| `npm run package` | 构建 native NSIS 安装包及 SHA-256 校验文件 |
| `npm run package:check` | 隔离验证安装、升级、快捷方式、注册信息和卸载 |
| `npm test` / `go test ./...` | Go 数据、原生界面和小窗口状态 / 绘制测试 |
| `npm run test:pi` | Pi 首字 / 失败 / 重试扩展的合成钩子与隐私验证 |
| `npm run native:check` | 五页、过渡帧、窗口控制及真实 Win32 小窗口验收 |
| `npm run widget:check` | 同一套原生 UI 验收，包含卡片、圆球、拖动、透明和错误重试 |
| `npm run test:ui` | 完整原生 UI 验收 |
| `npm run dev` / `go run .` | 从源码运行原生应用 |

测试使用 Go 生成的合成数据和临时目录。`artifacts/`、`build/` 和数据库均被 Git 忽略；源码结构见 [架构说明](docs/architecture.md)。

整体代码审查、算法改动、测试条件和优化前后指标见 [性能与代码简化报告](docs/performance.md)。

制作安装包另需 **NSIS 3 与 Unicode nsProcess 插件**。可使用 PATH 中的 `makensis.exe`，或通过 `MAKENSIS` / `NSIS_PLUGIN_DIR` 指定编译器与插件目录；脚本也会识别已有 electron-builder 的 NSIS 缓存。产物为 `release/Codex-Monitor-Setup-<版本>.exe` 和 `SHA256SUMS-<版本>.txt`，打包不依赖 electron-builder。

签名更新还使用 Windows 自带 `tar.exe`。`npm run package` 通过 `MYGO_UPDATER_PRIVATE_KEY`（MyGO 格式的 Base64 私钥）或 `MYGO_UPDATER_KEY_FILE` 读取签名密钥；本机默认位置是 `%LOCALAPPDATA%\codex-monitor\update-keys\mygo-update.key`。私钥不放入仓库，`mygo.json` 仅保存对应公钥。缺少密钥时仍可生成安装包，但不会生成签名更新。持续发布须保留同一私钥；同时上传安装包、`Codex-Monitor-<版本>-windows-amd64.tar.gz`、`update-windows-amd64.json` 和校验文件，全部资产就绪后再发布 Release。`--check-updates` 可直接验证更新源，不打开监测数据库。

<a id="指标口径"></a>

## 指标口径

- **用量来源**：只读扫描 `%CODEX_HOME%`（默认 `%USERPROFILE%\.codex`）的 `sessions/` 与 `archived_sessions/`，计入 Codex Desktop、VS Code Codex 插件和 Codex CLI（交互终端、`codex exec` 与 MonoCode）来源。新版按响应 ID 去重，旧累计记录采用差分；CLI 与 VS Code 差分记录按会话隔离。任务明细与 CSV 导出包含来源。升级后自动迁移数据库并重读已有日志，补采此前忽略的 CLI / VS Code 历史；保留已有统计、账号、价格和清空历史的时间边界。
- **pi 来源**：支持可识别的官方 Codex 登录来源。默认读取 `%USERPROFILE%\.pi\agent\sessions`，兼容 `openai-codex`，以及能通过当前本机官方 OAuth 配置识别的 `openai`。未知账号保持未归属；混用登录方式的历史日志需结合实际情况判断。
- **额度**：通过本机 Codex app-server 的 `account/rateLimits/read` 查询，属于当前账号，可能包括其他设备的用量。曲线在重置和采样空档处断开。
- **缓存命中率**：缓存输入 / 输入 Token；没有输入时保持未知。
- **实时 TPS**：最近 60 秒日志中的输出 Token / 60，包含等待时间。
- **首字时间（TTFT）**：逐次调用采用明确的客户端 TTFT 或 Pi provider 钩子测量；未采集时显示未知，不以相邻记录时间差估算。旧日志不能补算首字时间。
- **失败与重试**：只读 Codex 诊断数据库及可选的本机 OTel / Pi 事件；展示观测到的事件，缺少独立用量时 Token 和费用保持未知。未导出的内部重试不计入，事件数不等于完整网络重试次数。
- **费用**：按生效时间选择价格版本，显示 API 等值 USD 估算；不是订阅实付账单，未定价记录会明确标注。

近期日志每 3 秒增量扫描，全部目录每 60 秒校验；额度查询间隔可设为 60 / 120 / 300 秒。

<a id="数据与隐私"></a>

## 数据与隐私

统计和设置默认保存在 `%APPDATA%\codex-monitor`，不写入 Codex / pi 原始文件。不保存对话正文、工具调用、认证 token 或 API key；账号使用身份字段的 SHA-256 摘要。无外部遥测、无跨设备同步，不创建模型请求。可选的首字采集只在 `127.0.0.1` 接收字段白名单中的统计事件，不转发到外部；错误仅保存固定类别。

| 文件 / 目录 | 用途 |
| --- | --- |
| `monitor.sqlite` | Go 服务的统计、价格、账号和设置 |
| `monitor.sqlite-wal` / `monitor.sqlite-shm` | SQLite 运行文件 |
| `monitor-owner.json` | 数据库单进程所有权锁 |
| `widget-window.json` | 小窗口的位置、置顶和模式 |
| `native-shell/` | MyGO 窗口状态 |

已有数据库先以只读方式检查完整性，失败时显示具体原因并拒绝写入。可点击启动提示中的 **备份并修复**，程序会读取全部表并重建索引；仅在全部读取成功且完整性检查通过后替换数据库并自动重启。原库及 `-wal` / `-shm` 保留在数据目录的 `database-backups/recovery-<时间>-<随机值>/` 中，无需另装工具。若表本身无法完整读取，修复会失败并保留原库。清空历史保留价格和设置，并设置日志重放边界。

安装包中的主程序也支持命令行修复（先从托盘完全退出应用）：

```powershell
& "$env:LOCALAPPDATA\Programs\Codex Monitor\Codex Monitor Native.exe" --repair-database
```

如果启动提示数据库完整性检查失败，可先从托盘完全退出应用，备份 `monitor.sqlite` 及其 `-wal` / `-shm` 文件，再从源码目录运行恢复工具（需要 Go）：

```powershell
go run ./cmd/repair --source "$env:APPDATA\codex-monitor\monitor.sqlite" --output "$env:APPDATA\codex-monitor\monitor-recovered.sqlite"
```

工具在副本上读取全部表，绕过损坏的索引，保留尚未合并到主文件的已提交 WAL 数据，并重建索引；只有所有行读取成功且完整性检查通过才保留恢复文件，不覆盖原库。若表本身无法完整读取，恢复会失败并报告表名。恢复成功后，将原库及 `-wal` / `-shm` 移到备份目录，再将恢复文件改名为 `monitor.sqlite`，最后重新启动应用。

<details>
<summary>自定义目录与隔离运行</summary>

- `--data` / `MONITOR_TEST_DATA`：监测数据目录。
- `--home` / `MONITOR_CODEX_HOME`：Codex 来源目录。
- `--offline`：禁止自动扫描和额度查询。
- `PI_CODING_AGENT_DIR` / `PI_CODING_AGENT_SESSION_DIR`：pi 目录。
- `MONITOR_PI_HOME` / `MONITOR_PI_SESSIONS`：监测专用 pi 目录覆盖。

</details>

## 参考

- [MyGO 文档](https://mygo.egoist.dev/docs)
- [Codex App Server 协议](https://learn.chatgpt.com/docs/app-server)
- [OpenAI API 价格](https://developers.openai.com/api/docs/pricing)

<p align="center"><a href="#top">返回顶部 ↑</a></p>
