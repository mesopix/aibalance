#!/usr/bin/env node
/**
 * aibalance one-click installer / uninstaller (Windows, macOS, Linux).
 * Downloads the prebuilt binary from GitHub Releases; requires only Node.js.
 *
 * Usage:
 *   node install.js                          install the latest GitHub Release
 *   node install.js /path/to/aibalance       install from a local build
 *   curl -fsSL <url> | node                  remote install
 *   node install.js --uninstall              remove the binary, keep user data
 */
'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const https = require('https');
const { spawnSync } = require('child_process');

const OWNER = 'mesopix';
const REPO = 'aibalance';
const INSTALLER_URL = `https://raw.githubusercontent.com/${OWNER}/${REPO}/main/install.js`;

const USAGE = `用法：
  node install.js                          安装最新的 GitHub Release 版本
  node install.js /path/to/aibalance       安装本地构建的二进制
  node install.js --uninstall              卸载（只删除二进制，配置与缓存保留）`;

// Colors: only when writing to a terminal (and NO_COLOR is unset), so
// piped output and log files stay free of ANSI escape codes.
const colorFor = (stream) =>
  (stream.isTTY && !process.env.NO_COLOR && process.env.TERM !== 'dumb')
    ? { green: '\x1b[32m', yellow: '\x1b[33m', red: '\x1b[31m', reset: '\x1b[0m' }
    : { green: '', yellow: '', reset: '' };
const OUT = colorFor(process.stdout);
const ERR = colorFor(process.stderr);

function ok(msg) { console.log(`${OUT.green}✓ ${msg}${OUT.reset}`); }
function warn(msg) { console.log(`${OUT.yellow}⚠ ${msg}${OUT.reset}`); }

// ── Target platform ─────────────────────────────────────
const PLATFORM = process.platform; // win32 | darwin | linux
const IS_WINDOWS = PLATFORM === 'win32';
const GO_OS = IS_WINDOWS ? 'windows' : PLATFORM;

// Release assets are named after GOOS/GOARCH; only these two are published.
function targetArch() {
  if (process.arch === 'x64') return 'amd64';
  if (process.arch === 'arm64') return 'arm64';
  return null;
}
const GO_ARCH = targetArch();
const ASSET_NAME = GO_ARCH ? `aibalance-${GO_OS}-${GO_ARCH}${IS_WINDOWS ? '.exe' : ''}` : null;
const BINARY_NAME = IS_WINDOWS ? 'aibalance.exe' : 'aibalance';

// Shared per-user bin dir, the same convention claude/aider/uv use: one PATH
// entry serves every tool, instead of one entry per installed binary.
const HOME_DIR = process.env.USERPROFILE || os.homedir();
const INSTALL_DIR = path.join(HOME_DIR, '.local', 'bin');
const TARGET = path.join(INSTALL_DIR, BINARY_NAME);

// Retired Windows install layouts. %LOCALAPPDATA%\aibalance doubles as the
// live data directory (Chrome profiles, cache), so only a stray binary and
// the PATH entry are reported — never the directory itself.
const RETIRED_INSTALL_DIRS = IS_WINDOWS && process.env.LOCALAPPDATA
  ? [
      path.join(process.env.LOCALAPPDATA, 'aibalance'),
      path.join(process.env.LOCALAPPDATA, 'AICreditVisualizer'),
    ]
  : [];

function die(msg) {
  console.error(`${ERR.red}错误：${msg}${ERR.reset}`);
  if (DOWNLOADED_SELF && fs.existsSync(DOWNLOADED_SELF)) {
    console.error(`（安装脚本保留在 ${DOWNLOADED_SELF}，修复后可直接 node install.js 重试）`);
  }
  process.exit(1);
}

// The "curl -o install.js && node install.js" flow is the only mode that
// leaves install.js on disk (curl | node piping never writes a file). Detect
// that lone downloaded copy — but never one inside a repo.
const DOWNLOADED_SELF = (() => {
  const self = process.argv[1];
  if (!self) return null; // piped via stdin
  const resolved = path.resolve(self);
  if (path.basename(resolved).toLowerCase() !== 'install.js') return null;
  let dir;
  try {
    dir = fs.realpathSync(path.dirname(resolved));
  } catch (err) {
    return null;
  }
  if (dir !== fs.realpathSync(process.cwd())) return null;
  if (fs.existsSync(path.join(dir, 'go.mod'))) return null; // repo clone
  if (fs.existsSync(path.join(dir, '.git'))) return null; // repo clone
  return resolved;
})();

// Remove the downloaded installer after a successful run. On failure it is
// deliberately kept so the user can fix the problem and re-run directly.
function cleanupDownloadedSelf() {
  if (DOWNLOADED_SELF && fs.existsSync(DOWNLOADED_SELF)) {
    try {
      fs.unlinkSync(DOWNLOADED_SELF);
      ok(`已清理下载的安装脚本：${DOWNLOADED_SELF}`);
    } catch (err) {
      console.log(`  （安装脚本可手动删除：${DOWNLOADED_SELF}）`);
    }
  }
}

// ── Download (follows redirects, e.g. release asset CDN) ─
function download(url, redirectsLeft) {
  redirectsLeft = redirectsLeft === undefined ? 5 : redirectsLeft;
  return new Promise((resolve, reject) => {
    https.get(url, { headers: { 'User-Agent': 'aibalance-installer' } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume();
        if (redirectsLeft <= 0) return reject(new Error('重定向次数过多'));
        return resolve(download(res.headers.location, redirectsLeft - 1));
      }
      if (res.statusCode !== 200) {
        res.resume();
        const err = new Error(`HTTP ${res.statusCode}（${url}）`);
        err.statusCode = res.statusCode;
        return reject(err);
      }
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => resolve(Buffer.concat(chunks)));
    }).on('error', reject);
  });
}

// ── Source resolution ───────────────────────────────────
async function resolveSource(explicitPath) {
  if (explicitPath) {
    const localBuild = path.resolve(explicitPath);
    if (!fs.existsSync(localBuild)) die(`找不到文件：${localBuild}`);
    ok(`使用本地构建：${localBuild}`);
    return { data: fs.readFileSync(localBuild), label: '本地构建' };
  }
  if (!ASSET_NAME) {
    die(`暂不支持的架构 ${process.arch}：Release 只提供 amd64 与 arm64 产物。`);
  }
  console.log(`正在查询 ${OWNER}/${REPO} 的最新 Release …`);
  let release;
  try {
    release = JSON.parse((await download(`https://api.github.com/repos/${OWNER}/${REPO}/releases/latest`)).toString('utf8'));
  } catch (err) {
    if (err.statusCode === 404) {
      die('仓库还没有 Release：推送 v* 标签触发 GitHub Actions 构建后再试。');
    }
    if (err.statusCode === 403) {
      die('GitHub API 限流（未认证每小时 60 次），稍后重试。');
    }
    die(`查询 Release 失败：${err.message}`);
  }
  const asset = (release.assets || []).find((entry) => entry.name === ASSET_NAME);
  const available = (release.assets || []).map((entry) => entry.name).join('、') || '（无）';
  if (!asset) {
    die(`Release ${release.tag_name} 中没有 ${ASSET_NAME}（可能构建未完成）。\n已有产物：${available}`);
  }
  console.log(`正在下载 ${ASSET_NAME}（${release.tag_name}，${(asset.size / 1048576).toFixed(1)} MB）…`);
  try {
    const data = await download(asset.browser_download_url);
    return { data, label: release.tag_name };
  } catch (err) {
    die(`下载失败：${err.message}\n网络不通时可手动下载 ${ASSET_NAME}，再执行：node install.js /path/to/${ASSET_NAME}`);
  }
}

// ── Binary install (temp + rename, atomic) ──────────────
// Only rewrite the binary when its content differs, so re-install is a no-op.
function installBinary(source) {
  const installed = fs.existsSync(TARGET) ? fs.readFileSync(TARGET) : null;
  if (installed !== null && installed.equals(source)) {
    ok(`${BINARY_NAME} 已是最新：${TARGET}`);
    return false;
  }
  if (installed !== null) {
    warn(`已存在的 ${BINARY_NAME} 与安装源不同，将覆盖。`);
  }
  fs.mkdirSync(INSTALL_DIR, { recursive: true });
  // die() exits without running finally blocks, so clean the temp file up
  // explicitly before dying — otherwise it pollutes the bin directory.
  const temp = path.join(INSTALL_DIR, `.aibalance-${process.pid}.tmp`);
  fs.writeFileSync(temp, source);
  try {
    fs.renameSync(temp, TARGET);
  } catch (err) {
    try { fs.unlinkSync(temp); } catch (ignored) { /* best effort */ }
    die(`无法替换 ${TARGET}：${err.message}\naibalance 可能正在运行（TUI），退出后重试。`);
  }
  if (!IS_WINDOWS) {
    try {
      fs.chmodSync(TARGET, 0o755);
    } catch (err) {
      die(`无法设置可执行权限：${err.message}`);
    }
  }
  const sizeLabel = source.length >= 1048576
    ? `${(source.length / 1048576).toFixed(1)} MB`
    : `${Math.round(source.length / 1024)} KB`;
  ok(`已安装：${TARGET}（${sizeLabel}）`);
  return true;
}

// ── PATH: shared helpers ────────────────────────────────
const normalizeDir = (candidate) =>
  candidate.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();

// Compare PATH entries against a directory, tolerating %VAR%, $VAR and ~.
function dirEntryMatches(entry, dirPath) {
  const expanded = entry
    .replace(/%USERPROFILE%/gi, HOME_DIR)
    .replace(/%LOCALAPPDATA%/gi, process.env.LOCALAPPDATA || '')
    .replace(/\$HOME\b/g, HOME_DIR)
    .replace(/^~(?=\/|$)/, HOME_DIR)
    .replace(/["']/g, '');
  return normalizeDir(expanded) === normalizeDir(dirPath);
}

const separator = IS_WINDOWS ? ';' : ':';

function pathEnvContainsInstallDir() {
  return (process.env.PATH || '').split(separator).some((entry) => dirEntryMatches(entry, INSTALL_DIR));
}

// ── Windows PATH (HKCU\Environment) ─────────────────────
// Windows reg tooling: query the raw (unexpanded) value, rewrite it with the
// original type preserved, then broadcast WM_SETTINGCHANGE so new terminals
// see the change without logging off. setx is never used (1024-char limit).
function readUserPath() {
  const query = spawnSync('reg', ['query', 'HKCU\\Environment', '/v', 'Path'], { encoding: 'utf8' });
  if (query.status !== 0) {
    return { type: 'REG_EXPAND_SZ', entries: [] }; // value absent
  }
  const match = query.stdout.match(/^\s*Path\s+(REG_EXPAND_SZ|REG_SZ)\s*(.*)$/mi);
  if (!match) return { type: 'REG_EXPAND_SZ', entries: [] };
  const raw = match[2].trim();
  return { type: match[1], entries: raw ? raw.split(';') : [] };
}

function writeUserPath(type, entries) {
  const value = entries.join(';');
  const args = value
    ? ['add', 'HKCU\\Environment', '/v', 'Path', '/t', type, '/d', value, '/f']
    : ['delete', 'HKCU\\Environment', '/v', 'Path', '/f'];
  const write = spawnSync('reg', args, { encoding: 'utf8' });
  if (write.status !== 0) {
    die(`无法写入用户 PATH：${(write.stderr || write.stdout || '').trim()}`);
  }
}

// Idempotent: add the install dir exactly once, keep every other entry
// (including %VAR% references) untouched, in their original order.
function ensureWindowsPathEntry() {
  const { type, entries } = readUserPath();
  if (entries.some((entry) => dirEntryMatches(entry, INSTALL_DIR))) {
    ok(`用户 PATH 已包含：${INSTALL_DIR}`);
    return;
  }
  writeUserPath(type, [...entries, INSTALL_DIR]);
  broadcastEnvChange();
  ok(`已加入用户 PATH：${INSTALL_DIR}（重开终端后生效）`);
}

// Tell Explorer the environment changed so newly opened terminals inherit
// the updated PATH. Already-running terminals never see registry changes.
function broadcastEnvChange() {
  const command = [
    'Add-Type -MemberDefinition \'[DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Auto)]',
    'public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam,',
    'uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);\' -Name NativeMethods -Namespace Win32;',
    '$result = [UIntPtr]::Zero;',
    '[Win32.NativeMethods]::SendMessageTimeout([IntPtr]0xFFFF, 0x1A, [UIntPtr]::Zero, "Environment", 2, 5000, [ref]$result) | Out-Null',
  ].join(' ');
  const broadcast = spawnSync('powershell.exe', ['-NoProfile', '-Command', command], { encoding: 'utf8' });
  if (broadcast.status !== 0) {
    warn('环境变量变更广播失败，注销重新登录后 PATH 才会生效。');
  }
}

// ── Unix PATH (shell startup file, marked block) ────────
const PATH_MARKER_BEGIN = '# >>> aibalance >>>';
const PATH_MARKER_END = '# <<< aibalance <<<';

// Fish has no `export`; every other shell here reads POSIX syntax.
function pathBlockFor(configPath) {
  const exportLine = path.basename(path.dirname(configPath)) === 'fish'
    ? `set -gx PATH "$HOME/.local/bin" $PATH`
    : `export PATH="$HOME/.local/bin:$PATH"`;
  return [
    PATH_MARKER_BEGIN,
    '# Added by the aibalance installer',
    exportLine,
    PATH_MARKER_END,
    '',
  ].join('\n');
}

// Startup files to try, best first: the running shell's own file, then the
// common ones, then fish (different syntax, so it is never merged with POSIX).
function shellConfigCandidates() {
  const shellName = path.basename(process.env.SHELL || '');
  const profile = path.join(HOME_DIR, '.profile');
  if (shellName === 'fish') {
    return [path.join(HOME_DIR, '.config', 'fish', 'config.fish'), profile];
  }
  if (shellName === 'zsh') {
    return [path.join(HOME_DIR, '.zshrc'), profile];
  }
  if (shellName === 'bash') {
    return [path.join(HOME_DIR, '.bashrc'), profile];
  }
  return [
    path.join(HOME_DIR, '.bashrc'),
    path.join(HOME_DIR, '.zshrc'),
    profile,
    path.join(HOME_DIR, '.config', 'fish', 'config.fish'),
  ];
}

function stripPathBlock(contents) {
  const kept = [];
  let insideBlock = false;
  for (const line of contents.split('\n')) {
    if (line.trim() === PATH_MARKER_BEGIN) { insideBlock = true; continue; }
    if (line.trim() === PATH_MARKER_END) { insideBlock = false; continue; }
    if (!insideBlock) kept.push(line);
  }
  const trimmed = kept.join('\n').replace(/\s+$/, '');
  return trimmed ? `${trimmed}\n` : '';
}

function ensureUnixPathEntry() {
  if (pathEnvContainsInstallDir()) {
    ok(`PATH 已包含：${INSTALL_DIR}`);
    return;
  }
  const candidates = shellConfigCandidates();
  const configPath = candidates.find((candidate) => fs.existsSync(candidate))
    || path.join(HOME_DIR, '.profile');
  const previous = fs.existsSync(configPath) ? fs.readFileSync(configPath, 'utf8') : '';
  const block = pathBlockFor(configPath);
  fs.writeFileSync(configPath, `${stripPathBlock(previous).replace(/\n*$/, '\n')}\n${block}`);
  ok(`已写入 PATH 配置：${configPath}（重开终端后生效）`);
}

// Uninstall only removes our own marked block: other tools share this bin dir
// and the PATH entry belongs to the convention, not to aibalance.
function removeUnixPathEntry() {
  const removed = shellConfigCandidates()
    .concat([path.join(HOME_DIR, '.config', 'fish', 'config.fish')])
    .filter((candidate, index, all) => all.indexOf(candidate) === index)
    .filter((candidate) => {
      if (!fs.existsSync(candidate)) return false;
      const contents = fs.readFileSync(candidate, 'utf8');
      if (!contents.includes(PATH_MARKER_BEGIN)) return false;
      fs.writeFileSync(candidate, stripPathBlock(contents));
      return true;
    });
  if (removed.length > 0) {
    ok(`已移除 PATH 配置：${removed.join('、')}`);
  } else {
    console.log('未找到 aibalance 的 PATH 配置，跳过。');
  }
}

function ensurePathEntry() {
  if (IS_WINDOWS) ensureWindowsPathEntry();
  else ensureUnixPathEntry();
}

// ── Leftovers from retired install layouts (Windows) ────
// Reported only, never deleted: the directory may hold live Chrome profiles.
function warnLegacyLeftovers() {
  if (!IS_WINDOWS) return;
  for (const retiredDir of RETIRED_INSTALL_DIRS) {
    const strayBinary = path.join(retiredDir, BINARY_NAME);
    if (fs.existsSync(strayBinary)) {
      warn(`检测到旧安装位置的残留文件：${strayBinary}`);
      console.log('  确认无用后可手动删除（数据目录不受影响）：');
      console.log(`  del "${strayBinary}"`);
    }
    const deadEntries = readUserPath().entries.filter((entry) => dirEntryMatches(entry, retiredDir));
    if (deadEntries.length > 0) {
      warn(`用户 PATH 仍指向旧安装目录：${deadEntries.join(' ; ')}`);
      console.log('  可在「设置 → 系统 → 高级系统设置 → 环境变量」中手动移除该条目。');
    }
  }
}

// ── Chrome check (non-fatal; the TUI launcher needs it) ─
function chromeCandidates() {
  if (IS_WINDOWS) {
    return [
      process.env.ProgramFiles && path.join(process.env.ProgramFiles, 'Google', 'Chrome', 'Application', 'chrome.exe'),
      process.env['ProgramFiles(x86)'] && path.join(process.env['ProgramFiles(x86)'], 'Google', 'Chrome', 'Application', 'chrome.exe'),
      process.env.LOCALAPPDATA && path.join(process.env.LOCALAPPDATA, 'Google', 'Chrome', 'Application', 'chrome.exe'),
    ].filter(Boolean);
  }
  return [
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    path.join(HOME_DIR, 'Applications', 'Google Chrome.app', 'Contents', 'MacOS', 'Google Chrome'),
  ];
}

const CHROME_COMMANDS = ['google-chrome-stable', 'google-chrome', 'chromium-browser', 'chromium'];

function checkChrome() {
  if (chromeCandidates().some((candidate) => fs.existsSync(candidate))) return;
  const lookup = spawnSync('sh', ['-c', `command -v ${CHROME_COMMANDS.join(' ') || 'true'}`], { encoding: 'utf8' });
  if (lookup.status === 0 && lookup.stdout.trim()) return;
  warn('未检测到 Chrome：网页平台抓取依赖 Chrome。');
  console.log(IS_WINDOWS
    ? '  安装命令：winget install Google.Chrome'
    : PLATFORM === 'darwin'
      ? '  安装命令：brew install --cask google-chrome'
      : '  安装命令：sudo apt install google-chrome-stable（或发行版对应的 chromium 包）');
}

// ── Where the app keeps its data ────────────────────────
function dataDirectoryHint() {
  if (IS_WINDOWS) return '%LOCALAPPDATA%\\aibalance';
  return '${XDG_DATA_HOME:-~/.local/share}/aibalance';
}

function configFileHint() {
  if (IS_WINDOWS) return '%APPDATA%\\aibalance\\config.json';
  if (PLATFORM === 'darwin') return '~/Library/Application Support/aibalance/config.json';
  return '~/.config/aibalance/config.json';
}

// ── Install ─────────────────────────────────────────────
async function install(explicitPath) {
  const source = await resolveSource(explicitPath);
  const binaryChanged = installBinary(source.data);
  ensurePathEntry();
  checkChrome();
  warnLegacyLeftovers();

  console.log('');
  if (binaryChanged) {
    console.log(`${OUT.green}安装完成（${source.label}）。${OUT.reset}`);
  } else {
    console.log(`${OUT.green}已是最新（${source.label}），未替换文件。${OUT.reset}`);
  }
  console.log(`  重开一个终端运行 aibalance 启动 TUI（已打开的终端不会看到新的 PATH）。`);
  console.log('  首次启动会拉起自动化 Chrome 窗口，在窗口内登录各平台；');
  console.log(`  DeepSeek API Key 写入 ${configFileHint()} 的 deepseek_api_key（或运行 aibalance config 编辑）。`);
  cleanupDownloadedSelf();
}

// ── Uninstall ───────────────────────────────────────────
function uninstall() {
  if (fs.existsSync(TARGET)) {
    try {
      fs.unlinkSync(TARGET);
      ok(`已删除 ${TARGET}`);
    } catch (err) {
      die(`无法删除 ${TARGET}：${err.message}\naibalance 可能正在运行（TUI），退出后重试。`);
    }
  } else {
    console.log(`未找到 ${TARGET}，跳过。`);
  }
  if (IS_WINDOWS) warnLegacyLeftovers();
  else removeUnixPathEntry();
  console.log('');
  console.log(`${OUT.green}卸载完成。${OUT.reset}`);
  console.log(`  登录 profile 与用量缓存保留在 ${dataDirectoryHint()}，配置文件在 ${configFileHint()}。`);
  console.log(`  ${INSTALL_DIR} 是多个工具共用的目录，PATH 条目未做改动；可手动删除上述路径彻底清理。`);
  cleanupDownloadedSelf();
}

// ── Main ────────────────────────────────────────────────
async function main() {
  const args = process.argv.slice(2);
  if (args.includes('-h') || args.includes('--help')) {
    console.log(USAGE);
    console.log('\n远程一键安装：');
    console.log(`  curl -fsSL ${INSTALLER_URL} | node`);
    return;
  }
  if (!ASSET_NAME) {
    die(`暂不支持的架构 ${process.arch}：Release 只提供 amd64 与 arm64 产物。`);
  }
  if (args.includes('--uninstall') || args.includes('-u')) {
    uninstall();
    return;
  }
  await install(args.find((arg) => !arg.startsWith('-')));
}

main().catch((err) => die(err.message));
