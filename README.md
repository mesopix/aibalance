# AI Credit Visualizer

单语言 Go 项目：通过常驻 CDP Chrome 抓取各 AI 服务余额与用量，单一二进制 `aibalance` 提供 TUI 仪表盘与 `cli` 子命令两个入口。

当前支持的平台：

- DeepSeek Platform API：官方 `GET /user/balance`。
- BigModel GLM Coding Plan：通过已登录 Chrome profile 读取个人套餐用量页，展示 5 小时、每周额度、积分、活跃度、模型及 MCP 明细，并兼容旧版月度工具额度。
- Qwen Token Plan 个人版：通过已登录 Chrome profile 读取百炼控制台内部接口，展示 5 小时、7 天额度与订阅有效期，并在接口字段缺失时回退页面文本。
- Z.ai GLM Coding Plan（支持双账号）：通过已登录 Chrome profile 打开用量页，优先读取页面内部接口，失败时回退页面文本。
- ChatGPT Codex：尝试多个候选 Codex usage 入口，读取周额度、额外 Credits 与储存重置次数。
- Kimi Coding Plan：通过已登录 Chrome profile 抓取页面和接口候选数据。
- Qoder Team Credit：通过已登录 Chrome profile 抓取团队额度页面。

所有展示百分比统一使用 `left` 语义，即剩余额度比例。

## 目录

| 路径 | 说明 |
|------|------|
| `main.go` / `cli.go` / `render.go` | 单一入口 `aibalance`（仓库根目录，可直接 `go install .`）：默认启动 TUI 仪表盘（bubbletea + lipgloss，内嵌抓取与 Chrome 启动器）；`cli` 子命令输出 JSON / 人类可读文本 / 流式进度。 |
| `internal/aibalance/` | 抓取核心：服务注册、CDP 浏览器层、各服务解析、脱敏、视图层、缓存。 |
| `%LOCALAPPDATA%/AICreditVisualizer/` | 用量缓存 `latest_summary.json`、配置 `gui_settings.json`（含 DeepSeek Key 与 CDP 端点）、自动化 Chrome profiles。首次运行自动生成该目录与 `gui_settings.json`（模板嵌入 exe，源自 `config/gui_settings.json.example`）。 |

## 一键安装（新机器）

仅支持 Windows，需要 Node.js（跑 Claude Code 的机器必有）。任选其一：

```powershell
# 一行安装（下载 install.js 并直接运行，不留文件）
curl.exe -fsSL https://raw.githubusercontent.com/mesopix/aibalance/main/install.js | node

# 或分两步（失败时可直接 node install.js 重试）
curl.exe -fsSL -o install.js https://raw.githubusercontent.com/mesopix/aibalance/main/install.js
node install.js
```

安装器从 GitHub Releases 下载最新的 `aibalance-windows-amd64.exe`，放入 `%LOCALAPPDATA%\AICreditVisualizer\`，并把该目录写入用户 PATH（广播环境变更，重开终端后 `aibalance` 直接可用）。重复运行是幂等的：版本相同不替换文件、PATH 不重复添加。卸载：`node install.js --uninstall`（只删除 exe 与 PATH 条目，登录 profile、`gui_settings.json` 与缓存保留）。

Release 产物由推送 `v*` 标签触发 GitHub Actions 自动构建（`.github/workflows/release.yml`）。开发者验证本地构建时：`go build -trimpath -ldflags "-s -w" -o aibalance.exe .`，然后 `node install.js .\aibalance.exe`。

## 快速开始

```powershell
go build ./...
go run .                # TUI 仪表盘（自动启动/复用 CDP Chrome）
go run . cli --json     # CLI 子命令
go install .            # 安装 aibalance 到 $GOPATH/bin
```

TUI 启动时自动执行内置启动器：复用或启动常驻 CDP Chrome（主账号端口 `9222`，第二 z.ai 账号端口 `9333`，profile 位于 `%LOCALAPPDATA%\AICreditVisualizer\profiles\`），然后刷新全部服务。按 `r` 刷新、`q` 退出；`--once` 刷新一次即退出（脚本化用）。

## 配置（gui_settings.json）

`%LOCALAPPDATA%\AICreditVisualizer\gui_settings.json` 是唯一配置文件（首次运行自动生成，`cli` 子命令同样读取）。除 TUI 的服务开关与自动刷新外，还承载环境字段：

```json
{
  "auto_refresh": true,
  "schema_version": 2,
  "deepseek_api_key": "",
  "chrome_cdp_url": "http://127.0.0.1:9222",
  "chrome_cdp_url_2": "http://127.0.0.1:9333",
  "services": {
    "qwen_token_plan": { "enabled": true, "auto_refresh_interval_seconds": 120 },
    "chatgpt_codex": { "enabled": false, "auto_refresh_interval_seconds": 300 }
  }
}
```

- `enabled: false` 的服务不刷新、不渲染、不写入 `latest_summary.json`；未列出的服务默认启用。
- `auto_refresh: true` 时按各服务 `auto_refresh_interval_seconds`（默认 300 秒）独立定时刷新，同刻到期的服务合并为一批；缓存新鲜（<5 分钟）时启动直接显示缓存，各定时器从启动时刻起算。
- 手动 `r` 刷新全部启用服务并重置各定时器；文件缺失或非法时回退默认（全部启用、不自动刷新）。
- `deepseek_api_key` 填入 DeepSeek API Key（留空则跳过该服务）；`chrome_cdp_url` / `chrome_cdp_url_2` 是两个自动化 Chrome 的 CDP 端点（默认 9222 / 9333）。环境变量 `DEEPSEEK_API_KEY` / `CHROME_CDP_URL` / `CHROME_CDP_URL_2` 仍可覆盖文件值，显式 `--cdp-url` 优先级最高。
- 旧版 `.env.local` 会在启动时自动并入本文件（仅填空字段）并删除；含无法识别 key 的文件会保留并在 stderr 提示。
- 编辑方式：`aibalance config`（stdin 菜单）或 `aibalance config --edit`（用 `$EDITOR` 打开文件直接编辑，默认 notepad）；`aibalance -h` 列出全部子命令。

## 首次登录

网页平台需要先在专用 Chrome profile 里登录。启动 TUI 后会自动拉起自动化 Chrome 窗口，直接在该窗口里登录对应平台即可；登录态保存在 profile 中，之后无需重复登录。

## CLI

```powershell
go run . cli --json                        # 全部服务，JSON 输出
go run . cli --only deepseek_api --json    # 单个服务
go run . cli --only bigmodel_coding_plan,z_ai_coding_plan --json
go run . cli --json --progress-jsonl       # 流式进度事件
go run . cli --debug                       # 原始抓取数据（已脱敏）
go run . cli --cdp-url http://127.0.0.1:9222 --json
```

安装后（`go install .`）可直接使用 `aibalance cli ...`。

CLI 不启动 Chrome：需要 CDP 的服务要求常驻 Chrome 已在运行（TUI 启动器会做，或手动启动），否则该服务返回连接错误。

## 验证

```powershell
go vet ./...
go test ./...
```

`internal/aibalance` 的单测覆盖脱敏、时间格式化、各服务解析（含从 Python 实现移植的 fixture）与视图层。`AIBALANCE_LIVE_CDP=http://127.0.0.1:9222 go test ./internal/aibalance -run TestLiveBigmodelProbe -v` 可选地跑真实 Chrome 的集成探针。

网页平台改动后，建议先跑受影响的 `--only` 服务，再跑完整 CLI。

## 安全与保密

- 禁止提交 `.env*`、Chrome profile、快照、API Key 或私钥。
- 调试输出（`--debug`）经过防御性脱敏，但脱敏不能作为公开分享保证，不要提交或分享调试产物。
- 浏览器自动化使用专用 profile（`%LOCALAPPDATA%\AICreditVisualizer\profiles\`），而非日常浏览 profile。
