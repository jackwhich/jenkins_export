# Linux 编译指南

## 问题说明
在 macOS 上编译的二进制文件无法在 Linux 上运行，需要在 Linux 系统上重新编译。

## 方法 1：在 Linux 服务器上直接编译（推荐）

### 1. 确保已安装 Go
```bash
# 检查 Go 版本
go version

# 如果没有安装，安装 Go 1.24.0 或更高版本
# CentOS/RHEL:
sudo yum install golang -y

# Ubuntu/Debian:
sudo apt-get install golang-go -y
```

### 2. 克隆或上传代码到 Linux 服务器
```bash
# 如果代码已经在服务器上，直接进入目录
cd /path/to/jenkins_export

# 或者从 GitHub 克隆
git clone https://github.com/jackwhich/jenkins_export.git
cd jenkins_export
```

### 3. 编译
```bash
# 编译 Linux 版本
go build -o jenkins_exporter ./cmd/jenkins_exporter

# 或者指定架构（如果需要）
# 64位 Linux:
GOOS=linux GOARCH=amd64 go build -o jenkins_exporter ./cmd/jenkins_exporter

# ARM64 Linux:
GOOS=linux GOARCH=arm64 go build -o jenkins_exporter ./cmd/jenkins_exporter
```

### 4. 验证
```bash
# 检查文件类型
file jenkins_exporter
# 应该显示: jenkins_exporter: ELF 64-bit LSB executable, x86-64

# 测试运行
./jenkins_exporter --version
```

## 方法 2：在 macOS 上交叉编译（如果无法在 Linux 上编译）

### 在 macOS 上执行：
```bash
cd /Users/masheilapincamamaril/Desktop/jenkins_export/jenkins_exporter

# 编译 Linux 64位版本
GOOS=linux GOARCH=amd64 go build -o jenkins_exporter_linux ./cmd/jenkins_exporter

# 编译 Linux ARM64 版本（如果需要）
GOOS=linux GOARCH=arm64 go build -o jenkins_exporter_linux_arm64 ./cmd/jenkins_exporter
```

### 然后上传到 Linux 服务器：
```bash
# 使用 scp 上传
scp jenkins_exporter_linux user@your-server:/path/to/jenkins_export/jenkins_exporter
```

## 快速命令（在 Linux 服务器上执行）

```bash
# 一键编译脚本
cd /path/to/jenkins_export && \
go build -o jenkins_exporter ./cmd/jenkins_exporter && \
chmod +x jenkins_exporter && \
./jenkins_exporter --version
```

