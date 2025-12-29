#!/bin/bash

# Jenkins Exporter Linux 编译脚本
# 用法: ./build_linux.sh [amd64|arm64]

set -e

ARCH=${1:-amd64}
OUTPUT_DIR="dist"
BINARY_NAME="jenkins_exporter_linux_${ARCH}"

echo "开始编译 Linux ${ARCH} 版本..."

# 创建输出目录
mkdir -p ${OUTPUT_DIR}

# 编译
GOOS=linux GOARCH=${ARCH} go build -o ${OUTPUT_DIR}/${BINARY_NAME} ./cmd/jenkins_exporter

# 设置执行权限
chmod +x ${OUTPUT_DIR}/${BINARY_NAME}

# 显示文件信息
echo ""
echo "编译完成！"
echo "文件: ${OUTPUT_DIR}/${BINARY_NAME}"
file ${OUTPUT_DIR}/${BINARY_NAME}
ls -lh ${OUTPUT_DIR}/${BINARY_NAME}

echo ""
echo "可以使用以下命令测试："
echo "  ${OUTPUT_DIR}/${BINARY_NAME} --version"

