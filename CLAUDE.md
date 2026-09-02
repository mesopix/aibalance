# aibalance 项目指南

本文件是面向 Claude Code 的项目级上下文说明，也供其他协作者参考。

## 1. 项目概述

aibalance 是一个单语言 Go 项目：通过常驻 CDP Chrome 抓取各 AI 服务账号余额与用量，单一二进制 `aibalance` 提供 TUI 仪表盘与 `cli` 子命令两个入口。项目已从 Python CLI + C++ GUI 混合架构完成 Go 统一重写。

## 2. 目录结构

| 路径 | 说明 |
|------|------|
| `main.go` / `cli.go` / `render.go` | 单一入口 `aibalance`（位于仓库根目录，可直接 `go install .`）：默认启动 bubbletea + lipgloss 终端仪表盘（内嵌抓取与 Chrome 启动器，`r` 刷新、`l` 为需要登录的服务打开登录页、`q` 退出、`--once` 单次模式；卡片顶边框右侧是该服务的刷新状态位——正在刷新显示灰色 `⟳`，否则显示距上次刷新的耗时，并按已用刷新间隔比例着色：<30% 绿、30–80% 黄、≥80% 红，自动刷新关闭时保持灰色；无卡片的异常/需要登录行在刷新时把 detail 换成 `⟳ refreshing`，状态栏不再显示刷新进度；各卡片独立刷新——每服务一个独立 Cmd/context/超时，完成即合并显示与缓存，自动刷新按服务独立到期触发，`r` 跳过在飞服务；CLI 子命令保持串行）；`cli` 子命令提供 Python 兼容 CLI（`--only` / `--json` / `--progress-jsonl` / `--debug` / `--strict-exit`）；`config` 子命令（`config.go`）用 stdin 菜单交互编辑 `config.json`（`<n>` 切换开关、`<n> <秒>` 设间隔、`a` 切自动刷新、`s` 保存、`q` 退出，保存走 `SaveGUISettings` 原子写；`--edit` 改用 `$EDITOR`（默认 notepad）直接打开文件，退出后校验 JSON）；主入口 `-h` 由 `printMainUsage` 列出子命令。 |
| `internal/aibalance/` | 抓取核心包（见下）。 |
| `config/` | 配置原型目录：`gui_settings.json.example` 由 `embed.go` 以 `go:embed` 编译期嵌入 exe；用户目录缺文件时加载器把它物化成实体文件，example 即默认值单一来源（代码常量仅兜底，`deepseek_api_key` 在模板中必须保持空串惰性，由 `TestEmbeddedGUISettingsExampleMatchesRegistry` 强制）。文件格式为两层 `{meta: {version: "2"}, fields: {...}}`，由 go-config-manager v0.5.0 管理。 |
| `install.js` / `.github/workflows/release.yml` | 一键安装：推送 `v*` 标签触发 Actions 构建并发布 `aibalance-windows-amd64.exe` 到 Release；`install.js`（对齐 claude-code-statusline / git-config-sync 的安装器模式）下载 Release exe 到 `%LOCALAPPDATA%\aibalance\`、写用户 PATH（reg add + WM_SETTINGCHANGE 广播，禁用 setx）、支持 `curl \| node` 远程安装与 `--uninstall`（只删 exe 与 PATH 条目，数据保留；检测到旧 `AICreditVisualizer` 目录或旧 PATH 条目时只提示，不自动删除）。 |
| `%APPDATA%/aibalance/` | 配置文件 `config.json`（两层 `{meta, fields}` 格式，由 go-config-manager 管理；schema version 在 `meta.version`；任何子命令首启由嵌入 example 物化）。 |
| `%LOCALAPPDATA%/aibalance/` | 用量缓存 `latest_summary.json`（首次刷新后写出）、自动化 Chrome profiles（`profiles/ai-balance-chrome`、`profiles/ai-balance-chrome-2`，Chrome 首启创建）、遗留 `.env.local`（启动时一次性并入 settings 后删除，无法识别的 key 会保留文件并提示）。旧版 `gui_settings.json` 不再使用，新安装直接写入 `%APPDATA%` 下的 `config.json`。 |

`internal/aibalance` 内部结构：

| 文件 | 职责 |
|------|------|
| `service.go` | 服务注册表（`serviceRegistry`）、`Run` 串行调度（TUI 以每服务独立 `Run` 调用实现并发）、`SummarizeOutput`、进度事件。 |
| `browser.go` | CDP 连接（loopback 校验）、每服务专属常驻 tab（按 origin 认领，`acquireServicePage`）、`probeWebDashboard`（所有等待带超时封顶、同文档导航走 ignore-cache reload）、`makeWebDashboardRunner`、`OpenLoginPages`（TUI 按 `l` 打开登录页）。 |
| `chromelaunch.go` | Chrome 启动器：查找 chrome.exe、复用或 detached 启动 CDP Chrome、等待就绪。 |
| `collector.go` | JSON API 响应收集器（URL 关键字 + content-type 过滤、body 批量获取）。 |
| `deepseek.go` / `zai.go` / `qwen.go` / `kimi.go` / `qoder.go` / `codex.go` | 各服务的抓取与解析（`summarize*` 纯函数）。 |
| `privacy.go` | `RedactText` / `RedactData` 正则脱敏（对齐原 privacy.py）。 |
| `formatting.go` / `convert.go` | CST 时间格式化、lenient 数值转换。 |
| `view.go` | summary → `ServiceView` / `QuotaView` 结构化视图（CLI 与 TUI 共用）。 |
| `cache.go` / `envfile.go` / `guisettings.go` / `startup.go` | `latest_summary.json` 原子读写（复用 `writeUserDataFile`）、用户数据目录定位（`UserDataDirectory` 供缓存与遗留文件，`UserConfigDirectory` 供 config.json）与 `.env.local` 宽松解析（`parseEnvLocal` 纯函数）、`config.json` 加载/保存（通过 go-config-manager v0.5.0；缺文件时物化嵌入 example；坏文件返回 `*configmanager.CorruptConfigError`，所有入口 fatal 退出；enabled 过滤、自动刷新间隔、环境字段）、`LoadStartupSettings` 统一启动入口（物化 + 一次性 `.env.local` 迁移 + 把环境字段桥接为进程环境变量，真实环境变量不被覆盖）。 |

## 3. 构建与运行

```powershell
go build ./...
go run .                # TUI 仪表盘（自动启动/复用 CDP Chrome）
go run . cli --json     # CLI 子命令
go run . config         # 交互式编辑 config.json
go install .            # 安装 aibalance 到 $GOPATH/bin
```

TUI 内置启动器：主账号 Chrome 端口 `9222`，第二账号 Chrome 端口 `9333`（Z.ai Coding #2 与 BigModel Coding #2 共用该实例与 profile，两个站点 origin 不同、登录态互不影响），profile 在 `%LOCALAPPDATA%\aibalance\profiles\`。`cli` 子命令不启动 Chrome，依赖常驻 Chrome 已运行（可用 `--cdp-url` 指定）。

首次登录：启动 TUI 拉起自动化 Chrome 窗口后，直接在窗口里登录对应平台；登录态保存在 profile 中。

## 4. 验证方式

```powershell
go vet ./...
go test ./...
# 可选：真实 Chrome 集成探针
AIBALANCE_LIVE_CDP=http://127.0.0.1:9222 go test ./internal/aibalance -run TestLiveBigmodelProbe -v
# 单个服务调试
go run . cli --only deepseek_api --json --progress-jsonl
```

单测覆盖脱敏、时间格式化、各服务解析（含从 Python 实现移植的 fixture）、视图层与缓存。网页平台改动后，先跑受影响的 `--only` 服务，再跑完整 CLI。

## 5. 编码规范

- Go 标准风格（gofmt / go vet 必须干净）。
- 注释使用英文，专业、简洁，尽量控制在 3 行以内。
- 禁止使用单字母变量；变量与函数名应具有描述性。
- Service ID 使用 snake_case，例如 `qoder_team_credit`、`z_ai_coding_plan`。
- 用户可见的百分比统一使用 `left`（剩余）语义。
- 输出 schema 字段与原 Python 实现保持兼容（`latest_summary.json` 的消费方依赖它）。

### Quota 窗口 label 与排序规范

- 窗口 label 按等效时长统一：等效 5 小时的滚动窗口一律显示 `5h`（Kimi 的 300 分钟窗口、z.ai/BigModel/Qwen 的 `five_hour`）；等效 7 天的周窗口一律显示 `7d`（各家的 `weekly`，含 Kimi 与 Codex）。月度窗口使用语义名（`monthly tools`），Qoder 的总额度行使用 `all`。渲染层对 label 宽度有硬上限（`render.go` 的 `maxQuotaLabelWidth`，超宽 panic），新增更长的 label 时必须同步调大该常量与 `quotaLabelColumns`。
- 同一服务内 quota 行按窗口时长升序排列：`5h` 在 `7d` 之上，`7d` 在月度之上。
- label 与顺序在 view 层（`internal/aibalance/view.go` 的 `format*View`）以静态字符串写死；解析层与渲染层不做任何 label 换算、时长比较或排序逻辑，拿到什么就显示什么。新增 provider 时按本规范直接写死正确的 label 与顺序。

## 6. 提交与 PR 规范

- 提交信息使用祈使句，简洁，例如 `Add z.ai coding plan usage`、`Migrate browser services to Go`。
- 单次提交只包含一个功能或修复。
- PR 需说明：改动内容、原因、验证命令、浏览器相关服务的登录/Profile 假设。

## 7. 安全与保密

- 禁止提交 `.env*`、Chrome Profile、快照、浏览器 Trace、API Key 或私钥。DeepSeek Key 落在用户目录的 `config.json`，模板中的 `deepseek_api_key` 必须保持空串（`TestEmbeddedGUISettingsExampleMatchesRegistry` 强制）。
- 调试输出（`--debug`）必须脱敏；脱敏仅作为纵深防御，不能把调试产物视为可公开内容。
- 浏览器自动化使用专用 Profile（`%LOCALAPPDATA%\aibalance\profiles\`），而非日常浏览 Profile。
- **用户目录已从 `AICreditVisualizer` 改名为 `aibalance`**（`appName` 常量与 `UserDataDirectory()` 共用它，见 `internal/aibalance/guisettings.go` 与 `envfile.go`），旧目录**不做自动迁移**：升级后配置回到嵌入 example 的默认值、Chrome profile 重建、网页服务需重新登录。`install.js` 检测到旧目录或旧 PATH 条目时只打印提示，不删除任何东西。
- CDP 端点仅允许 loopback（`assertLoopbackCDPURL` 强制）。

## 8. Agent 工作流约定

- 本项目中的构建与测试命令（`go build` / `go test` / `go vet` / `go run`）可由 Agent 直接执行，无需每次单独确认。
- 涉及文件写入、编辑、删除前，若要求或方案不明确，应主动向用户提出关键问题。
- 编辑操作仅修改必要代码，避免引入大量无关行尾变更。

## 9. 已知注意事项

- **rod 的 `browser.Close()` 会终止整个 Chrome 进程**（不同于 Playwright 的 `stop()`）。连接常驻 Chrome 后不要调用 Close，进程退出即断开。
- **rod 的 `EachEvent` 返回的 wait 函数必须被调用**（goroutine 驱动）才会派发事件，与 Playwright `page.on()` 注册即派发不同。
- **`chrome://` 等浏览器内部页面跨进程导航会使 session 失效**，tab 认领时跳过特殊页面。
- **禁止用 rod 的 `browser.Pages()` 找 tab**：它会对 Chrome 里每个 tab 都 attach session 并重新 Emulate（改写视口与 UA），attach 到正在跨进程导航的 tab 时 CDP 调用可能永不返回，只能等 pass context 到期。`claimServiceTarget` 改为直接读 `Target.getTargets` 的 target info（带 URL，不 attach），只对认领到的那个 target 调 `PageFromTarget`。
- 每个浏览器服务在各自 Chrome 里有一个专属常驻 tab（`claimServiceTarget` 按 origin 认领，找不到才新建且不关闭）。tab 已在目标页面时 `probeWebDashboard` 用 ignore-cache reload，避免同 URL 导航命中缓存恢复而不触发 API；其余情况正常导航。probe 内所有等待（导航事件、load、数据就绪、Eval、响应 body 获取）都有超时封顶，页面持续轮询或有挂起请求时不会卡满整个 pass。
- **不要用 rod 的 `WaitRequestIdle` 等 SPA 加载完成**：它的时长参数是"最短静默时长"而非超时上限，所以每个服务至少白等 10 秒；而阿里云百炼控制台这类持续轮询的页面永远凑不出静默窗口，只能把 `TimeoutMS` 预算整个烧光。`waitForDashboardData` 改为等各服务在 summarizer 旁声明的必需 API 响应（`qwenRequiredResponses` 等）落地即走，并以"抓取计数停止增长 `captureQuietWindow`"兜底——同一 summarizer 的不同 host 未必调用全部接口（BigModel 没有 `model-usage`，z.ai 未开 usageBoard 时不发 `monitor/*`）。必需响应全部到手时跳过 `WaitMS` 静置延迟：那段延迟是为了等叫不出名字的迟到 XHR。实测单次并发 pass：Qwen 33.1s→5.7s、Kimi 33.1s→2.6s、Qoder 20.3s→3.8s、BigModel 17.6s→7.9s、z.ai 30.3s→15.4s。
- 只有未声明必需响应的服务（`chatgpt_codex`）仍走 `WaitRequestIdle`：它的用量既来自 API 也来自页面文本与 profile 菜单，没有固定的响应集合可等。它顺序尝试 8 个候选 URL，每个都付完整导航预算，因此整体远超 `refreshTimeout`；已在 needs_login 时短路（同一 session 下后续候选只会重复跳登录页），但登录态下无用量信号时仍会跑满 8 个候选。
- TUI 各服务独立并发刷新：`claimServiceTarget` 由包级互斥锁串行化，避免并发认领同一 origin 时重复创建常驻 tab；锁内只有 `pageClaimTimeout` 封顶的 browser 级调用，attach 放在锁外，单个卡住的 tab 不会连带堵死其他服务；单服务刷新超时 2 分钟（`refreshTimeout`），卡住时报错而非一直转圈；`EnsureCDPChromeReady` 由包级互斥锁串行化"检查+启动+等待"，避免并发 ensure 重复拉起 Chrome；每次 probe 独立建 CDP 连接、collector 按 page 隔离，无其他跨服务共享状态。
- 网页服务的抓取结果仍受 SPA 渲染时序影响（原 Python 实现同样如此）。解析逻辑以单测 fixture 为准。
- z.ai 使用 session cookie（浏览器关闭即失效）；其他服务多为持久 cookie。Chrome 重启后 z.ai 需要重新登录。
- **配置文件损坏时所有入口（TUI、cli、config 子命令）统一打印含文件路径的错误并以非零码退出**，不再沿用旧的"警告后用默认值继续"行为。修复流程预留但尚未实现（库侧 `RepairAppConfig` 为桩接口）。
