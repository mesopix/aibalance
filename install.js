#!/usr/bin/env node
/**
 * aibalance one-click installer / uninstaller (Windows).
 * Downloads the prebuilt exe from GitHub Releases; requires only Node.js.
 *
 * Usage:
 *   node install.js                          install the latest GitHub Release
 *   node install.js C:\path\aibalance.exe    install from a local build
 *   curl -fsSL <url> | node                  remote install
 *   node install.js --uninstall              remove the exe and the PATH entry
 */
'use strict';

const fs = require('fs');
const path = require('path');
const https = require('https');
const { spawnSync } = require('child_process');

const OWNER = 'mesopix';
const REPO = 'aibalance';
const ASSET_NAME = 'aibalance-windows-amd64.exe';
const RELEASE_API = `https://api.github.com/repos/${OWNER}/${REPO}/releases/latest`;
const INSTALLER_URL = `https://raw.githubusercontent.com/${OWNER}/${REPO}/main/install.js`;

const USAGE = `用法：
  node install.js                          安装最新的 GitHub Release 版本
  node install.js C:\\path\\aibalance.exe    安装本地构建的 exe
  node install.js --uninstall              卸载（删除 exe 与 PATH 条目，数据保留）`;

// Colors: only when writing to a terminal (and NO_COLOR is unset), so
// piped output and log files stay free of ANSI escape codes.
const colorFor = (stream) =>
  (stream.isTTY && !process.env.NO_COLOR && process.env.TERM !== 'dumb')
    ? { green: '\x1b[32m', yellow: '\x1b[33m', red: '\x1b[31m', reset: '\x1b[0m' }
    : { green: '', yellow: '', red: '', reset: '' };
const OUT = colorFor(process.stdout);
const ERR = colorFor(process.stderr);

function ok(msg) { console.log(`${OUT.green}✓ ${msg}${OUT.reset}`); }
function warn(msg) { console.log(`${OUT.yellow}⚠ ${msg}${OUT.reset}`); }

// The Windows flow "curl.exe -o install.js && node install.js" is the only
// mode that leaves install.js on disk (curl | node piping never writes a
// file). Detect that lone downloaded copy — but never one inside a repo.
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

function die(msg) {
  console.error(`${ERR.red}错误：${msg}${ERR.reset}`);
  if (DOWNLOADED_SELF && fs.existsSync(DOWNLOADED_SELF)) {
    console.error(`（安装脚本保留在 ${DOWNLOADED_SELF}，修复后可直接 node install.js 重试）`);
  }
  process.exit(1);
}

// ── Install locations ───────────────────────────────────
if (process.platform !== 'win32') {
  die('aibalance 仅支持 Windows（依赖 CDP Chrome 与 %LOCALAPPDATA% profile）。');
}
if (!process.env.LOCALAPPDATA) {
  die('环境变量 LOCALAPPDATA 未设置，无法确定安装目录。');
}
const INSTALL_DIR = path.join(process.env.LOCALAPPDATA, 'AICreditVisualizer');
const TARGET = path.join(INSTALL_DIR, 'aibalance.exe');

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
  console.log(`正在查询 ${OWNER}/${REPO} 的最新 Release …`);
  let release;
  try {
    release = JSON.parse((await download(RELEASE_API)).toString('utf8'));
  } catch (err) {
    if (err.statusCode === 404) {
      die(`仓库还没有 Release：推送 v* 标签触发 GitHub Actions 构建后再试。`);
    }
    if (err.statusCode === 403) {
      die('GitHub API 限流（未认证每小时 60 次），稍后重试。');
    }
    die(`查询 Release 失败：${err.message}`);
  }
  const asset = (release.assets || []).find((entry) => entry.name === ASSET_NAME);
  if (!asset) {
    die(`Release ${release.tag_name} 中没有资产 ${ASSET_NAME}（可能构建未完成），稍后重试。`);
  }
  console.log(`正在下载 ${ASSET_NAME}（${release.tag_name}，${(asset.size / 1048576).toFixed(1)} MB）…`);
  try {
    const data = await download(asset.browser_download_url);
    return { data, label: release.tag_name };
  } catch (err) {
    die(`下载失败：${err.message}\n网络不通时可手动下载 ${ASSET_NAME}，再执行：node install.js C:\\path\\${ASSET_NAME}`);
  }
}

// ── Exe install (temp + rename, atomic) ─────────────────
// Only rewrite the exe when its content differs, so a re-install is a no-op.
function installExe(source) {
  const installed = fs.existsSync(TARGET) ? fs.readFileSync(TARGET) : null;
  if (installed !== null && installed.equals(source)) {
    ok(`aibalance.exe 已是最新：${TARGET}`);
    return false;
  }
  if (installed !== null) {
    warn('已存在的 aibalance.exe 与安装源不同，将覆盖。');
  }
  fs.mkdirSync(INSTALL_DIR, { recursive: true });
  // die() exits without running finally blocks, so clean the temp file up
  // explicitly before dying — otherwise it pollutes the data directory.
  const temp = path.join(INSTALL_DIR, `.aibalance-${process.pid}.exe.tmp`);
  fs.writeFileSync(temp, source);
  try {
    fs.renameSync(temp, TARGET);
  } catch (err) {
    try { fs.unlinkSync(temp); } catch (ignored) { /* best effort */ }
    die(`无法替换 ${TARGET}：${err.message}\naibalance 可能正在运行（TUI），退出后重试。`);
  }
  const sizeLabel = source.length >= 1048576
    ? `${(source.length / 1048576).toFixed(1)} MB`
    : `${Math.round(source.length / 1024)} KB`;
  ok(`已安装：${TARGET}（${sizeLabel}）`);
  return true;
}

// ── User PATH (HKCU\Environment) ────────────────────────
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

// Match both expanded and %LOCALAPPDATA%-based spellings of the install dir.
function isInstallDirEntry(entry) {
  const expanded = entry.replace(/%LOCALAPPDATA%/gi, process.env.LOCALAPPDATA);
  const normalize = (candidate) => candidate.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();
  return normalize(expanded) === normalize(INSTALL_DIR);
}

// Idempotent: add the install dir exactly once, keep every other entry
// (including %VAR% references) untouched, in their original order.
function ensurePathEntry() {
  const { type, entries } = readUserPath();
  if (entries.some(isInstallDirEntry)) {
    ok(`用户 PATH 已包含：${INSTALL_DIR}`);
    return;
  }
  writeUserPath(type, [...entries, INSTALL_DIR]);
  broadcastEnvChange();
  ok(`已加入用户 PATH：${INSTALL_DIR}（重开终端后生效）`);
}

function removePathEntry() {
  const { type, entries } = readUserPath();
  const matches = entries.filter(isInstallDirEntry);
  if (matches.length === 0) {
    console.log('用户 PATH 中没有安装目录条目，跳过。');
    return;
  }
  writeUserPath(type, entries.filter((entry) => !isInstallDirEntry(entry)));
  broadcastEnvChange();
  ok(`已从用户 PATH 移除：${matches[0]}`);
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

// ── Chrome check (non-fatal; the TUI launcher needs it) ─
function checkChrome() {
  const candidates = [
    process.env.ProgramFiles && path.join(process.env.ProgramFiles, 'Google', 'Chrome', 'Application', 'chrome.exe'),
    process.env['ProgramFiles(x86)'] && path.join(process.env['ProgramFiles(x86)'], 'Google', 'Chrome', 'Application', 'chrome.exe'),
    path.join(process.env.LOCALAPPDATA, 'Google', 'Chrome', 'Application', 'chrome.exe'),
  ].filter(Boolean);
  if (candidates.some((candidate) => fs.existsSync(candidate))) return;
  warn('未检测到 Chrome：网页平台抓取依赖 Chrome。安装命令：winget install Google.Chrome');
}

// ── Install ─────────────────────────────────────────────
async function install(explicitPath) {
  const source = await resolveSource(explicitPath);
  const exeChanged = installExe(source.data);
  ensurePathEntry();
  checkChrome();

  console.log('');
  if (exeChanged) {
    console.log(`${OUT.green}安装完成（${source.label}）。${OUT.reset}`);
  } else {
    console.log(`${OUT.green}已是最新（${source.label}），未替换文件。${OUT.reset}`);
  }
  console.log('  重开一个终端运行 aibalance 启动 TUI（已打开的终端不会看到新的 PATH）。');
  console.log('  首次启动会拉起自动化 Chrome 窗口，在窗口内登录各平台；');
  console.log('  DeepSeek API Key 写入 %LOCALAPPDATA%\\AICreditVisualizer\\gui_settings.json 的 deepseek_api_key。');
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
  removePathEntry();
  console.log('');
  console.log(`${OUT.green}卸载完成。登录 profile、gui_settings.json 与缓存保留在 ${INSTALL_DIR}，可手动删除整个目录彻底清理。${OUT.reset}`);
  cleanupDownloadedSelf();
}

// ── Main ────────────────────────────────────────────────
async function main() {
  const args = process.argv.slice(2);
  if (args.includes('-h') || args.includes('--help')) {
    console.log(USAGE);
    console.log(`\n远程一键安装：`);
    console.log(`  curl.exe -fsSL ${INSTALLER_URL} | node`);
    return;
  }
  if (args.includes('--uninstall') || args.includes('-u')) {
    uninstall();
    return;
  }
  await install(args.find((arg) => !arg.startsWith('-')));
}

main().catch((err) => die(err.message));
