# OpenXiaoAI Patch - Go 实现

使用 Golang 实现的 OTA 固件获取模块，替代原有的 Node.js/TypeScript 实现。包含完整的 patches、extract、patch、squashfs 脚本，可独立完成固件制作。

## 功能

- 小米账号登录（支持账号密码或 passToken）
- 获取小爱音箱设备信息
- 获取 OTA 固件下载链接
- 下载固件到 `assets/` 目录
- 固件提取、打补丁、打包（与 client-patch 相同流程）

## 调试

设置环境变量 `MI_DEBUG=true` 可输出详细调试日志，便于排查登录、设备匹配、OTA 请求等问题。

## 构建

```bash
cd packages/client-patch-go
go build -o ota ./cmd/ota
```

## 使用

### 1. 配置环境变量

复制 `.env.example` 为 `.env` 并填写：

```bash
cp .env.example .env
# 编辑 .env 填写 MI_USER, MI_PASS, MI_DID
```

### 2. 单独运行 OTA（下载固件）

从 `client-patch` 目录运行（需要 .env 和 assets 目录）：

```bash
cd ../client-patch
../client-patch-go/ota
```

### 3. 完整构建流程

从 `client-patch` 目录执行完整固件制作流程：

```bash
cd ../client-patch
# 使用 Go 版 OTA 下载固件
../client-patch-go/ota
# 后续步骤使用原有脚本
npm run extract
npm run patch
npm run squashfs
```

或使用提供的 build 脚本：

```bash
cd packages/client-patch-go
./build.sh
```

## 与 client-patch 的集成

Go 版 OTA 与原有 Node 版输出兼容，会写入：
- `assets/.model` - 设备型号
- `assets/.version` - 固件版本
- `assets/*.bin` - 下载的固件文件

可直接替换 `npm run ota` 为 `../client-patch-go/ota` 使用。
