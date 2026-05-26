# tools/client-patch

使用 Golang 实现的小爱音箱固件补丁工具。包含完整的 OTA 下载、固件提取、打补丁、打包流程，单二进制完成固件制作。

> [!CAUTION]
> 刷机有风险，操作需谨慎。请勿下载使用不明来历的固件！

## 功能

- 小米账号登录（支持账号密码或 passToken）
- 获取小爱音箱设备信息
- 获取 OTA 固件下载链接
- 下载固件到 `assets/` 目录
- 固件提取、打补丁、打包

## 下载预制补丁固件

如果你只是想直接刷机使用，可以在 [GitHub Releases](https://github.com/idootop/open-xiaoai/releases) 页面下载已打好补丁的官方固件：

- [Xiaomi 智能音箱 Pro v1.58.6](https://github.com/idootop/open-xiaoai/releases/tag/OH2P_1.58.6)
- [小爱音箱 Pro v1.94.13](https://github.com/idootop/open-xiaoai/releases/tag/LX06_1.94.13)

> [!TIP]
> 每个 release 都有两个文件，下载 `patched` 那个：
>
> - `xxx_patched.squashfs` 打补丁后的固件
> - `xxx.squashfs` 原版固件（可用来刷回原系统）

> [!NOTE]
> 默认 SSH 登录密码为 `open-xiaoai`，如需修改请自行制作固件。

> [!IMPORTANT]
> 请下载和你当前小爱音箱版本一致的固件，跨版本刷机可能会出现未知错误，导致设备变砖。
> 如果上面没有你的版本，请升级设备固件到最新版本，或者按照下面的教程自行制作固件。

> [!CAUTION]
> 当前支持的最新固件版本为：
>
> - Xiaomi 智能音箱 Pro 👉 [v1.58.6](https://github.com/idootop/open-xiaoai/releases/tag/OH2P_1.58.6)
> - 小爱音箱 Pro 👉 [v1.94.13](https://github.com/idootop/open-xiaoai/releases/tag/LX06_1.94.13)
>
> 更新版本的固件可能存在变化，导致刷机失败，设备变砖，请自行评估风险。

下载到 `xxx_patched.squashfs` 后，按照 [`docs/flash.md`](../../docs/flash.md) 进行刷机即可。

## 自行制作固件

### 环境依赖

- Go 1.21+
- `python3`、`squashfs-tools`（提供 `unsquashfs` / `mksquashfs`）、`patch`、`openssl`、`file`、`xz-utils`

macOS：

```bash
brew install squashfs python3
```

Ubuntu / Debian：

```bash
sudo apt-get install -y python3 squashfs-tools file patch openssl xz-utils
```

### 1. 配置环境变量

复制 `.env.example` 为 `.env` 并填写：

```bash
cp .env.example .env
# 编辑 .env 填写 MI_USER, MI_PASS, MI_DID
```

### 2. 单独运行 OTA（仅下载固件）

```bash
cd tools/client-patch
go build -o ota ./cmd/ota
./ota
```

### 3. 完整构建流程（OTA + 提取 + 补丁 + 打包）

```bash
cd tools/client-patch
./build.sh
# 产物在 assets/ 目录 
assets/xxx/root-patched.squashfs
assets/xxx/root.squashfs
```

`build.sh` 会自动构建 `ota` 二进制，依次执行 `cmd/ota`、`src/extract.sh`、`src/patch.sh`、`src/squashfs.sh`。

### 4. 跳过账号登录（CI 友好）

如果你已经通过其他方式拿到了 OTA 信息，可以直接通过 `OTA` 环境变量传入 JSON，跳过小米账号登录：

```bash
OTA='{"url":"https://...","model":"LX06","version":"1.94.13"}' ./ota
```

仓库内的 `.github/workflows/{LX06,OH2P}.yaml` 就是这种模式：通过 `workflow_dispatch` 输入 OTA JSON，CI 直接构建并发布到 Releases。

## 自定义启动脚本

默认修改后的补丁固件，会将 `/data/init.sh` 文件作为启动脚本，开机时自动运行。如果你需要自定义开机启动脚本，可自行创建和修改该文件。

示例：

```bash
#!/bin/sh

/usr/sbin/tts_play.sh '初始化成功'
```

## 调试

设置环境变量 `MI_DEBUG=true` 可输出详细调试日志，便于排查登录、设备匹配、OTA 请求等问题。

## 输出格式

完整流程结束后会生成：

- `assets/.model` - 设备型号
- `assets/.version` - 固件版本
- `assets/*.bin` - 下载的原始固件文件
- `assets/<MODEL>_<VERSION>/root.squashfs` - 解包出的原版根分区
- `assets/<MODEL>_<VERSION>/root-patched.squashfs` - 打补丁后的根分区
