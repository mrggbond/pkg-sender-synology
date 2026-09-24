# 发布说明

[English release notes](RELEASE_NOTES.md)

## 0.1.0-0021 - Synology NAS/SPK MVP

这个版本将 Synology NAS 实现打包为 DSM 7 SPK，并在 DS1517+ x86_64 目标环境完成验证。

### 主要功能

- Synology DSM 原生套件（`PKGSenderNAS`），包含 DSM UI 入口。
- Web UI 浏览 NAS 上的 PS5 PKG 文件。
- 可在 Web UI 中配置 PS5 receiver IP。
- 可在 Web UI 中配置 PKG Library 路径，支持多个 NAS 路径。
- 支持多库递归扫描，并使用不透明包 ID。
- 通过 `POST http://<ps5>:12800/api/install` 向 PS5 receiver 提交安装任务。
- 通过 Go `http.ServeContent` 提供 `GET|HEAD /pkg/{id}`，支持 Range 请求。
- 解析包元数据，包括标题、本地化标题、Title ID、Content ID、版本和包类型。
- 按 Title ID 聚合游戏家族，并按 Game / Patch / DLC / App 排序。
- 本地标题别名导入/导出流程，用于手工整理中文标题。
- 持久化安装历史和 FIFO 队列，支持重试、取消和调整顺序。
- 显示 UDP receiver 发现状态。
- Web UI 支持中文 / 英文切换。

### 不包含

- Synology SPK MVP 不包含 PS4 / GoldHEN 工作流。
- 认证、TLS 和公网暴露刻意不在范围内。请仅在可信局域网内使用。

### 已验证目标

- Synology DSM 7.2.2
- DS1517+ / x86_64
- SPK：`PKGSenderNAS-0.1.0-0021-x86_64.spk`
- ARMv8 SPK 已完成交叉构建和结构校验：`PKGSenderNAS-0.1.0-0021-armv8.spk`

### Release assets

- `PKGSenderNAS-0.1.0-0021-x86_64.spk`
- `PKGSenderNAS-0.1.0-0021-armv8.spk`
- `pkg-receiver.elf`
- Docker 镜像：`ghcr.io/mrggbond/pkg-sender-synology:0.1.0-0021`

### SHA256

```text
277626ae1a6715c729a187e3d5102032eb4a1cc93c892f00710863bda66c7c33  PKGSenderNAS-0.1.0-0021-x86_64.spk
d946c43e6b12c6945f41e3008159acee5d232f10b06e7d8b67b940ff5488569a  PKGSenderNAS-0.1.0-0021-armv8.spk
f281fde2d62353cfd0b263b48815bdae64bc2527d5ded2e2ff9956d9c87b1cb2  pkg-receiver.elf
```
