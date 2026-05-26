#!/usr/bin/env bash

set -e

BASE_DIR=$(pwd)
WORK_DIR=$BASE_DIR/temp

PASSWORD=${SSH_PASSWORD:-"open-xiaoai"}
PASSWORD=$(openssl passwd -1 -salt "open-xiaoai" "$PASSWORD")

# 应用指定目录下的补丁文件
apply_patches() {
    local patch_dir="$1"
    local message="$2"
    
    echo "🔥 $message"
    
    if [ -d "$patch_dir" ]; then
        for file in "$patch_dir"/*; do
            if [ -f "$file" ]; then
                if [[ "$file" == *.patch ]]; then
                    echo "  📝 [patch] 应用 $(basename "$file")"
                    local temp_patch=$(mktemp)
                    sed "s|{SSH_PASSWORD}|$PASSWORD|g" "$file" > "$temp_patch"
                    patch -p1 < "$temp_patch"
                    rm "$temp_patch"
                elif [[ "$file" == *.sh ]]; then
                    echo "  📜 [patch] 执行 $(basename "$file")"
                    bash "$file"
                fi
            fi
        done
    else
        echo "  ⚠️ [patch] 目录不存在: $patch_dir"
    fi
}

if [ ! -f "$BASE_DIR/assets/.model" ]; then
    echo "❌ 固件信息不存在，请先下载固件到：$BASE_DIR/assets/"
    exit 1
fi

PATCH_DIR=$BASE_DIR/patches
MODEL=$(cat $BASE_DIR/assets/.model)
echo "📝 [patch] 设备型号: $MODEL"

cd $WORK_DIR/squashfs-root

apply_patches "$PATCH_DIR" "正在应用通用补丁..."
apply_patches "$PATCH_DIR/$MODEL" "正在应用 $MODEL 补丁..."

echo "✅ [patch] 补丁应用完成"
