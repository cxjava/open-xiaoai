#!/usr/bin/env bash

set -e

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$SCRIPT_DIR"

# 构建 OTA 二进制（如不存在）
if [ ! -f "$SCRIPT_DIR/ota" ]; then
  echo "🔨 [build] 构建 OTA 二进制..."
  go build -o ota ./cmd/ota
fi

# 1. 使用 Go 版 OTA 下载固件
echo "📥 [build] 步骤 1/4: 获取固件..."
./ota

# 2. 提取固件
echo "📦 [build] 步骤 2/4: 提取固件..."
bash src/extract.sh

# 3. 打补丁
echo "📝 [build] 步骤 3/4: 应用补丁..."
bash src/patch.sh

# 4. 打包固件
echo "📦 [build] 步骤 4/4: 打包固件..."
bash src/squashfs.sh

echo "✅ [build] 打包完成，固件文件已保存到 assets 目录"
