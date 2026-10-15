# PKG Sender for Synology

[English README](README.md)

这是 [`Loopayeh/pkg-sender`](https://github.com/Loopayeh/pkg-sender) 的 Synology NAS / DSM SPK 专用 fork。

本 fork 将一个轻量级 Go NAS 服务打包成 Synology DSM 原生套件，让运行 `pkg-receiver.elf` 的 PS5 可以在局域网内直接从 Synology NAS 拉取并安装 PKG 文件。本项目面向可信局域网环境使用。

## 当前版本

最新正式版本：**[v0.1.3](https://github.com/mrggbond/pkg-sender-synology/releases/tag/v0.1.3)**。

请根据 **DSM 系统版本和 NAS CPU 架构**选择安装包：

| Release 附件 | 适用平台 |
| --- | --- |
| `PKGSenderNAS-0.1.3-x86_64.spk` | DSM 7，x86_64（Intel/AMD） |
| `PKGSenderNAS-0.1.3-armv8.spk` | DSM 7，armv8（ARM64） |
| `PKGSenderNAS-0.1.3-dsm6-x86_64.spk` | DSM 6，x86_64（Intel/AMD） |
| `pkg-receiver.elf` | 可选的 PS5 接收端 Payload，需在 PS5 端单独加载 |

三个 v0.1.3 SPK 均已通过本地 Go 测试、构建及包结构校验。**尚未完成 v0.1.3 的真实 NAS 安装和 DSM 图标显示验收。**此前的真实硬件验收是在 DSM 7.2.2 / DS1517+（x86_64）上使用旧版完成；DSM 6 和 DSM 7 ARM64 仍待硬件验证。

## v0.1.3 更新内容

- **未知类型 PKG 与 DLC 一起默认折叠**，现有“展开/收起 DLC”按钮的数量包含两种类型；展开后仍保留“未知”文件类型标识。
- 搜索可显示匹配的 DLC 和未知类型文件；按游戏独立保存展开状态，自动刷新不会丢失状态。
- DSM6、DSM7 安装包均包含此改进，中英文界面一致。

## 延续此前版本的功能

- **DLC 默认按游戏折叠**，每个游戏独立展开/收起；自动刷新不会丢失展开状态，搜索 DLC 时仍显示匹配文件。
- **状态栏只显示 PS5 在线/离线**（绿色/红色），移除 NAS 在线标识；PS5 IP 输入及保存功能保持不变。
- 修复 DSM 应用菜单入口配置，补齐不同尺寸的应用图标；点击后打开 `http://<NAS-IP>:9898/ui/`。
- **DSM6 和 DSM7 均可安装后直接启动**，通过 Web UI 完成首次配置，无需手工编辑 `config.env`。DSM7 升级保留已有配置，对旧版占位符做运行时兼容。

## 功能

- DSM 6 x86_64 与 DSM 7 x86_64/armv8 原生 SPK，无需依赖 Container Manager。
- DSM 应用菜单快捷入口，也可直接访问 `http://<NAS-IP>:9898/ui/`。
- 在 Web UI 配置 PS5 IP 和多个 NAS PKG 库目录。
- 按 Title ID 聚合游戏；游戏本体、补丁和应用正常显示，DLC 与未知类型文件默认折叠。
- 递归扫描 PKG、HTTP Range/HEAD 文件服务、PKG 元数据解析（标题、多语言标题、ID、版本、Game/Patch/DLC 分类）。
- 本地标题别名导入/导出，用于人工整理游戏中文名。
- 持久化安装历史和 FIFO 顺序队列，支持重试、仅取消排队任务、队列上下调整。
- 通过 UDP 信标发现 PS5 接收端；**界面只显示已配置 PS5 的在线/离线状态**。
- 中文 / 英文 Web UI 切换。

## 安装和首次使用

1. 从 [v0.1.3 Release](https://github.com/mrggbond/pkg-sender-synology/releases/tag/v0.1.3) 下载对应 SPK，在 DSM **套件中心手动安装**。
2. **DSM 6：**友好版默认 PS5 IP 为空，PKG 目录位于套件自身的数据目录，可以在尚未配置 PS5 时启动。打开 `http://<NAS-IP>:9898/ui/`，填写 **PS5 IP**、**PKG 库路径**并保存。未显式设置公网下载地址时，服务从 HTTP 请求的 Host 推导下载 URL。
3. **DSM 7：**从 v0.1.2 起也能不配置就启动。通过 DSM 应用菜单或 `http://<NAS-IP>:9898/ui/` 打开 Web UI，保存 **PS5 IP** 和 **PKG 库路径**即可；首次安装不再要求修改 `config.env`。升级保留已有配置，旧的 `CHANGE_ME_*` 占位符仅在运行时处理。若自定义目录不可访问，仍需修复路径或权限。
4. 通过平时使用的 PS5 WebKit / exploit 流程在 PS5 上加载 `pkg-receiver.elf`。接收端监听 TCP `12800`，NAS 通过 UDP `12801` 接收在线信标。
5. 点击 DSM 应用菜单图标，或直接打开 `http://<NAS-IP>:9898/ui/`，选择 PKG 并点击**安装**；PS5 会通过 HTTP 从 NAS 拉取 PKG。

**NAS 不负责向 PS5 发送或加载 Payload**，ELF 需要在 PS5 端单独加载。本项目面向可信局域网使用。请给套件的系统内部用户授予 PKG 共享目录的读取和遍历权限。**不要同时让 Docker 和原生 SPK 监听相同的 TCP 9898 端口。**

详细配置、升级及兼容性说明请看 [SPK 文档](nas/spk/README.md)。

## Docker

多架构 Docker 镜像发布在 GitHub Container Registry。详见 [docs/docker.zh-CN.md](docs/docker.zh-CN.md)。

```sh
docker pull ghcr.io/mrggbond/pkg-sender-synology:latest
```

## 本地构建

原生 SPK 脚本位于 `nas/spk/`，需要 Go 1.24 或更新版本。在仓库根目录执行：

```sh
# DSM 7 x86_64
GO_BIN=/path/to/go ./nas/spk/build.sh

# DSM 7 ARM64
ARCH=armv8 GO_BIN=/path/to/go ./nas/spk/build.sh

# DSM 6 x86_64
GO_BIN=/path/to/go ./nas/spk/build-dsm6.sh

# Web UI 独立回归测试
node --test nas/internal/httpserver/ui.test.cjs
```

产物输出到 `nas/spk/dist/`，默认版本为 `0.1.3`。SPK 构建不等于 Docker 镜像同步发布。

## 配置

原生 SPK 的可编辑配置位于 `/var/packages/PKGSenderNAS/var/config.env`。**DSM6 和 DSM7 v0.1.2 均提供免预配置首次启动**：使用套件自有空目录、空 PS5 IP，启动后在 Web UI 中配置。如需固定 NAS 公共地址或设置其他高级选项，可手工修改：

```sh
PKGSENDER_PUBLIC_BASE_URL="http://192.168.1.20:9898"
PKGSENDER_PS5_IP="192.168.1.50"
PKGSENDER_PS5_PORT="12800"
PKGSENDER_PACKAGE_DIR="/volume1/PS5/PKG"
# 可选：多目录覆盖设置
# PKGSENDER_PACKAGE_DIRS='["/volume1/PS5/PKG","/volume1/More/PKG"]'
```

默认公共下载地址为空时，服务会从 HTTP 请求 Host 构造 PKG 下载 URL，因此应使用 PS5 可访问的 NAS 局域网地址打开 Web UI。设置 `PKGSENDER_PACKAGE_DIRS` 后，优先使用多目录配置；旧的单目录 `PKGSENDER_PACKAGE_DIR` 仍兼容。升级不会覆盖已有配置，共享目录权限仍需由 DSM 配置。

## 文档

详细 NAS 实现说明见 [`nas/README.md`](nas/README.md)。SPK 构建和 DSM 迁移说明见 [`nas/spk/README.md`](nas/spk/README.md)。

## 与上游项目的关系

本仓库是 `Loopayeh/pkg-sender` 的 fork，上游项目使用 MIT License。

这个 Synology 专用实现是独立项目，不隶属于上游作者，也未获得上游作者赞助或背书。上游归属说明保留在 [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) 和仓库根目录 [`LICENSE`](LICENSE) 中。

## License

MIT。详见 [`LICENSE`](LICENSE)。
