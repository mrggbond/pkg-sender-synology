# PKG Sender for Synology

[English README](README.md)

这是 [`Loopayeh/pkg-sender`](https://github.com/Loopayeh/pkg-sender) 的 Synology NAS / DSM SPK 专用 fork。

本 fork 将一个轻量级 Go NAS 服务打包成 Synology DSM 原生套件，让运行 `pkg-receiver.elf` 的 PS5 可以在局域网内直接从 Synology NAS 拉取并安装 PKG 文件。本项目面向可信局域网环境使用。

## 当前版本

当前已验证的 Synology 构建版本：`0.1.0-0021`。

已验证目标环境：

- Synology DSM 7.2.2
- DS1517+ / x86_64
- PS5 receiver 接口：`http://<ps5-ip>:12800/api/install`

## 功能

- Synology DSM 原生套件（`PKGSenderNAS`）。
- DSM 应用入口，可直接打开 Web UI。
- Web UI 浏览 PKG 库并发起安装。
- 可在 Web UI 中配置 PS5 IP。
- 可在 Web UI 中配置 PKG Library 路径，支持多个 NAS 目录。
- 递归扫描 PKG，使用稳定的不透明包 ID。
- 支持 HTTP Range 和 HEAD 的文件服务。
- 解析 PS5 PKG 元数据，包括标题、本地化标题、Title ID、Content ID、版本和包类型。
- 按 Title ID 聚合游戏家族。
- 本地标题别名导入/导出流程，用于手工整理中文标题。
- 持久化安装历史和 FIFO 队列，支持重试、取消、调整顺序。
- 显示 PS5 receiver UDP 发现状态。
- Web UI 支持中文 / 英文切换。

## 工作方式

1. 通过你常用的 PS5 WebKit / exploit 流程在 PS5 上加载 `pkg-receiver.elf`。
2. receiver 在 PS5 上监听 `12800` 端口。
3. 在 NAS 上运行 Synology SPK。
4. 在 Web UI 中配置 PS5 IP 和一个或多个 PKG Library 路径。
5. 点击安装。NAS 向 PS5 receiver 提交安装请求，PS5 再通过 HTTP 从 NAS 拉取 PKG。

NAS 不负责发送或加载 PS5 payload。payload 加载不属于这个 SPK 的职责范围。

## Docker

多架构 Docker 镜像发布在 GitHub Container Registry。详见 [docs/docker.zh-CN.md](docs/docker.zh-CN.md)。

```sh
docker pull ghcr.io/mrggbond/pkg-sender-synology:latest
```

## 构建

Synology 套件位于 `nas/spk`。

```sh
cd nas
ARCH=x86_64 GO_BIN=/path/to/go ./spk/build.sh
```

输出文件位于：

```text
nas/spk/dist/PKGSenderNAS-<version>-x86_64.spk
```

ARMv8 构建目标：

```sh
cd nas
ARCH=armv8 GO_BIN=/path/to/go ./spk/build.sh
```

## 配置

原生 SPK 将运行时配置保存在 DSM 套件 app-data 目录：

```text
/var/packages/PKGSenderNAS/var/config.env
```

重要配置项：

```sh
PKGSENDER_PUBLIC_BASE_URL="http://<nas-ip>:9898"
PKGSENDER_PS5_IP="<ps5-ip>"
PKGSENDER_PS5_PORT="12800"
PKGSENDER_PACKAGE_DIR="/volume1/PS5/PKG"
PKGSENDER_PACKAGE_DIRS='["/volume1/PS5/PKG","/volume1/More/PKG"]'
```

如果存在 `PKGSENDER_PACKAGE_DIRS`，会优先使用它。`PKGSENDER_PACKAGE_DIR` 仍然兼容旧的单库部署。

## 文档

详细 NAS 实现说明见 [`nas/README.md`](nas/README.md)。SPK 构建和 DSM 迁移说明见 [`nas/spk/README.md`](nas/spk/README.md)。

## 与上游项目的关系

本仓库是 `Loopayeh/pkg-sender` 的 fork，上游项目使用 MIT License。

这个 Synology 专用实现是独立项目，不隶属于上游作者，也未获得上游作者赞助或背书。上游归属说明保留在 [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) 和仓库根目录 [`LICENSE`](LICENSE) 中。

## License

MIT。详见 [`LICENSE`](LICENSE)。
