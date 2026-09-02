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
| `~/.local/bin/` | 安装位置（`aibalance`，Windows 上为 `aibalance.exe`）。多个工具共用的目录，因此只需要一条 PATH 条目。 |
| 配置目录 | `%APPDATA%\aibalance\config.json`（Windows）／`~/Library/Application Support/aibalance/config.json`（macOS）／`~/.config/aibalance/config.json`（Linux），含 DeepSeek Key 与 CDP 端点。任何子命令首次运行时由嵌入二进制的模板物化，模板源自 `config/gui_settings.json.example`。 |
| 数据目录 | `%LOCALAPPDATA%\aibalance\`（Windows）／`$XDG_DATA_HOME` 或 `~/.local/share/aibalance`（Unix）：用量缓存 `latest_summary.json`（首次刷新成功后写出）与自动化 Chrome profiles（`profiles/ai-balance-chrome`、`profiles/ai-balance-chrome-2`，Chrome 首启创建）。 |

## 一键安装（新机器）

支持 Windows / macOS / Linux（amd64 与 arm64），需要 Node.js（跑 Claude Code 的机器必有）。

```bash
# 一行安装（下载 install.js 并直接运行，不留文件）
curl -fsSL https://raw.githubusercontent.com/mesopix/aibalance/main/install.js | node

# 或分两步（失败时可直接 node install.js 重试）
curl -fsSL -o install.js https://raw.githubusercontent.com/mesopix/aibalance/main/install.js
node install.js
```

Windows 上用 `curl.exe -fsSL ...` 代替 `curl`（避免用到 PowerShell 的 `curl` 别名）。

安装器从 GitHub Releases 下载匹配当前平台的产物，放入 **`~/.local/bin/`**（与 claude、aider、uv 等工具同一约定），并在 PATH 里没有该目录时补上一条：

- **Windows**：写入 `HKCU\Environment` 的 `Path` 并广播 `WM_SETTINGCHANGE`（不用 `setx`，它有 1024 字符上限）。
- **macOS / Linux**：向当前 shell 的启动文件（`~/.bashrc` / `~/.zshrc` / `~/.profile` / fish 的 `config.fish`，取第一个已存在的；都不存在则新建 `~/.profile`）追加一段带 `# >>> aibalance >>>` 标记的 `export PATH=...`。

重复运行是幂等的：内容相同不替换文件，标记块已存在则跳过。卸载：`node install.js --uninstall`（删除二进制并移除标记块；Windows 上不动 PATH 条目，因为该目录由多个工具共享）。

> 从 `AICreditVisualizer` 时代升级的用户：旧安装目录 `%LOCALAPPDATA%\AICreditVisualizer` 与旧的 PATH 条目安装器不会自动删除，只会在结束时提示，请手动清理。配置与登录 profile 不会自动迁移——新目录是一套全新数据，网页平台需要重新登录。

Release 产物由推送 `v*` 标签触发 GitHub Actions 交叉编译（`.github/workflows/release.yml`）：`windows-amd64`、`linux-amd64`、`linux-arm64`、`darwin-amd64`、`darwin-arm64`。开发者验证本地构建时：`go build -o aibalance .`，然后 `node install.js ./aibalance`。

## 快速开始

```powershell
go build ./...
go run .                # TUI 仪表盘（自动启动/复用 CDP Chrome）
go run . cli --json     # CLI 子命令
go install .            # 安装 aibalance 到 $GOPATH/bin
```

TUI 启动时自动执行内置启动器：复用或启动常驻 CDP Chrome（主账号端口 `9222`，第二 z.ai 账号端口 `9333`，profile 位于数据目录的 `profiles\` 下），然后刷新全部服务。Chrome 的查找按平台进行：Windows 查 `ProgramFiles` / `LOCALAPPDATA` 下的 `chrome.exe`，macOS 查 `/Applications/Google Chrome.app`，Linux 在 PATH 里找 `google-chrome` / `chromium`。按 `r` 刷新、`q` 退出；`--once` 刷新一次即退出（脚本化用）。

## 配置（config.json）

唯一的配置文件（首次运行自动生成，`cli` 子命令同样读取）在系统的用户配置目录里，与安装位置无关：`%APPDATA%\aibalance\config.json`（Windows）／`~/Library/Application Support/aibalance/config.json`（macOS）／`~/.config/aibalance/config.json`（Linux）。除 TUI 的服务开关与自动刷新外，还承载环境字段：

```json
{
  "meta": { "version": "2" },
  "fields": {
    "auto_refresh": false,
    "deepseek_api_key": "",
    "chrome_cdp_url": "http://127.0.0.1:9222",
    "chrome_cdp_url_2": "http://127.0.0.1:9333",
    "services": {
      "qwen_token_plan": { "enabled": true, "auto_refresh_interval_seconds": 120 },
      "chatgpt_codex": { "enabled": false, "auto_refresh_interval_seconds": 300 }
    }
  }
}
```

`services` 里未列出的服务按「启用 + 默认 300 秒」处理；`aibalance config` 保存时会把全部已知服务写全。

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
- 浏览器自动化使用专用 profile（数据目录的 `profiles\` 下），而非日常浏览 profile。
