# NshSend

局域网文件与文本传输桌面应用，兼容 [LocalSend Protocol v2.2](https://github.com/localsend/protocol)，可与 LocalSend 官方客户端（桌面端、iOS、Android）互相发现并收发文件和文本。

## 功能

- 自动发现同一局域网内的 LocalSend 设备（UDP 组播 `224.0.0.167:53317`），支持手动输入 IP 添加设备
- 发送 / 接收文件、文件夹与文本，HTTPS 传输并校验对端证书指纹
- 接收确认与 PIN 校验，传输历史记录
- 关闭窗口隐藏到系统托盘，后台持续接收
- 界面语言：简体中文 / English，跟随设置即时切换

## 架构

```
┌────────────────────────┐   stdin/stdout NDJSON IPC   ┌──────────────────────┐
│  app/  (NSH TSX UI)    │ ◄─────────────────────────► │  core/  (Go sidecar) │
│  界面、托盘、i18n       │                             │  发现、收发、协议实现  │
└────────────────────────┘                             └──────────────────────┘
```

- **`core/`** — Go 实现的协议核心，仅使用标准库（零第三方依赖，`CGO_ENABLED=0`），编译为 `bin/nshsend-core`。stdout 专用于 IPC 帧，日志写 stderr 与滚动 `core.log`。
- **`app/`** — [Native Shell (NSH)](https://nsh.dev) TSX 桌面界面，按业务垂直切片组织（`receive` / `send` / `history` / `settings`），共享 UI 组件在 `app/src/ui/`。
- **`locales/app.yml`** — 界面文案（zh-CN + en）。
- **`scripts/`** — 构建、开发与打包脚本。

## 环境要求

- [nsh CLI](https://nsh.dev) ≥ 0.7
- Go ≥ 1.24（构建 core）
- Linux 运行时需要 GTK 4.14+ 与 WebKitGTK；文件/文件夹选择优先使用 xdg-desktop-portal，回退 `zenity` / `kdialog`
- Linux 托盘需要 SNI 宿主面板（KDE Plasma、waybar；GNOME 需 AppIndicator 扩展）

## 开发

```bash
./scripts/dev.sh        # 构建 core 并启动应用
```

验证：

```bash
cd core && go vet ./... && go test -race ./...
nsh check               # TSX 类型检查
nsh test                # UI 测试
```

## 打包（Linux AppImage）

```bash
./scripts/package-appimage.sh
```

产物为 `.nsh/out/NshSend-x86_64.AppImage`。脚本先构建 core，再调用 `nsh package --target linux --format appimage`，将 `nshsend-core` 注入 AppDir 并通过 `AppRun` 的 `PATH` 使 UI 能找到它，最后用 `appimagetool` 重打包（缺失时自动下载到 `bin/`）。

AppImage 不包含 GTK 4 / WebKitGTK，目标系统需已安装。无 FUSE 的环境可用 `APPIMAGE_EXTRACT_AND_RUN=1` 运行。

## 目录结构

```
app/          NSH TSX 界面（App.tsx 入口、src/ 业务切片、icons/、tray.svg）
core/         Go 协议核心（cmd/ 入口、internal/ 各业务模块）
locales/      i18n 文案
scripts/      build-core.sh / dev.sh / package-appimage.sh
bin/          构建产物（nshsend-core、appimagetool）
nsh.json      NSH 应用清单（窗口、能力声明）
```