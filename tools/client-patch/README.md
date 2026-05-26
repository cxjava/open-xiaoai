# tools/client-patch

使用 Golang 实现的小爱音箱固件补丁工具。包含完整的 OTA 下载、固件提取、打补丁、打包流程，可单独完成固件制作（不再需要 Node.js）。

> 这是 [legacy/client-patch-ts](../../legacy/client-patch-ts/README.md) 的 Go 重写版本。TS 版仍保留供 CI 兼容，但日常使用推荐本目录下的 Go 版。

## 功能

- 小米账号登录（支持账号密码或 passToken）
- 获取小爱音箱设备信息
- 获取 OTA 固件下载链接
- 下载固件到 `assets/` 目录
- 固件提取、打补丁、打包

## 调试

设置环境变量 `MI_DEBUG=true` 可输出详细调试日志，便于排查登录、设备匹配、OTA 请求等问题。

## 构建与使用

### 1. 配置环境变量

复制 `.env.example` 为 `.env` 并填写：

```bash
cp .env.example .env
# 编辑 .env 填写 MI_USER, MI_PASS, MI_DID
```

### 2. 单独运行 OTA（下载固件）

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
```

`build.sh` 会自动构建 `ota` 二进制，依次执行 `cmd/ota`、`src/extract.sh`、`src/patch.sh`、`src/squashfs.sh`。

## 输出格式

`ota` 与 TS 版输出兼容，会写入：
- `assets/.model` - 设备型号
- `assets/.version` - 固件版本
- `assets/*.bin` - 下载的固件文件

完整流程结束后会生成：
- `assets/<MODEL>_<VERSION>/root.squashfs` - 原版固件
- `assets/<MODEL>_<VERSION>/root-patched.squashfs` - 打补丁后的固件
