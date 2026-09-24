# Docker 镜像

[English](docker.md)

Docker 镜像打包的是与 Synology SPK 相同的 Go NAS 服务。它适用于普通 Linux 服务器、Synology Container Manager、Portainer、Unraid、TrueNAS SCALE，以及其它与 PS5 位于同一局域网内的 Docker 主机。

## 镜像地址

```text
ghcr.io/mrggbond/pkg-sender-synology
```

发布平台：

```text
linux/amd64
linux/arm64
```

## 快速启动

```sh
docker run -d \
  --name pkg-sender-nas \
  --restart unless-stopped \
  -p 9898:9898 \
  -v /volume1/PS5/PKG:/packages:ro \
  -e PKGSENDER_PACKAGE_DIR=/packages \
  -e PKGSENDER_LISTEN=:9898 \
  -e PKGSENDER_PUBLIC_BASE_URL=http://192.168.1.20:9898 \
  -e PKGSENDER_PS5_IP=192.168.1.50 \
  -e PKGSENDER_PS5_PORT=12800 \
  ghcr.io/mrggbond/pkg-sender-synology:latest
```

打开：

```text
http://192.168.1.20:9898/ui/
```

需要替换：

- `/volume1/PS5/PKG`：宿主机上的 PKG 文件目录。
- `192.168.1.20`：PS5 能访问到的 NAS/服务器局域网 IP。
- `192.168.1.50`：正在运行 `pkg-receiver.elf` 的 PS5 局域网 IP。

## Docker Compose

```yaml
services:
  pkg-sender-nas:
    image: ghcr.io/mrggbond/pkg-sender-synology:latest
    container_name: pkg-sender-nas
    restart: unless-stopped
    read_only: true
    ports:
      - "9898:9898"
    volumes:
      - "/volume1/PS5/PKG:/packages:ro"
    environment:
      PKGSENDER_PACKAGE_DIR: /packages
      PKGSENDER_LISTEN: ":9898"
      PKGSENDER_PUBLIC_BASE_URL: "http://192.168.1.20:9898"
      PKGSENDER_PS5_IP: "192.168.1.50"
      PKGSENDER_PS5_PORT: "12800"
```

## 多个 PKG 库路径

把多个宿主机目录挂载到不同的容器路径，并用 JSON 数组设置 `PKGSENDER_PACKAGE_DIRS`。

```yaml
services:
  pkg-sender-nas:
    image: ghcr.io/mrggbond/pkg-sender-synology:latest
    container_name: pkg-sender-nas
    restart: unless-stopped
    read_only: true
    ports:
      - "9898:9898"
    volumes:
      - "/volume1/PS5/Base:/library/base:ro"
      - "/volume1/PS5/Updates:/library/updates:ro"
      - "/volume1/PS5/DLC:/library/dlc:ro"
    environment:
      PKGSENDER_PACKAGE_DIRS: '["/library/base","/library/updates","/library/dlc"]'
      PKGSENDER_LISTEN: ":9898"
      PKGSENDER_PUBLIC_BASE_URL: "http://192.168.1.20:9898"
      PKGSENDER_PS5_IP: "192.168.1.50"
      PKGSENDER_PS5_PORT: "12800"
```

存在 `PKGSENDER_PACKAGE_DIRS` 时会优先使用它；单目录部署仍可继续使用 `PKGSENDER_PACKAGE_DIR`。

## Payload 说明

Docker 镜像不会发送或加载 `pkg-receiver.elf`。你需要先通过自己的 PS5 WebKit/exploit 流程在 PS5 上加载 ELF，然后再用 Web UI 向 `12800` 端口上的 receiver 发送安装请求。

## 本地构建

```sh
cd nas
docker build -t pkg-sender-nas:local .
```

使用 Buildx 构建多架构镜像：

```sh
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t ghcr.io/mrggbond/pkg-sender-synology:latest \
  --push \
  ./nas
```
