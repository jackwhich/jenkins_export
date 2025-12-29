# 如何运行示例代码

## 方法1: 直接运行（推荐）

### 运行完整示例
```bash
# 修改代码中的 URL、用户名、密码
go run example_get_jobs.go
```

### 运行简化示例
```bash
# 1. 编辑 example_get_jobs_simple.go，取消注释要运行的函数
# 2. 修改代码中的 URL、用户名、密码
go run example_get_jobs_simple.go
```

### 运行测试脚本（带命令行参数）
```bash
go run test_job_sdk.go \
  -url http://jenkins.example.com \
  -user your_username \
  -pass your_password \
  -folder prod-gray-ebpay \
  -recursive
```

## 方法2: 先编译再运行

```bash
# 编译
go build example_get_jobs.go

# 运行
./example_get_jobs
```

## 方法3: 使用环境变量（更安全）

```bash
# 设置环境变量
export JENKINS_URL="http://jenkins.example.com"
export JENKINS_USERNAME="your_username"
export JENKINS_PASSWORD="your_password"

# 运行（需要修改代码读取环境变量）
go run example_get_jobs.go
```

## 快速开始

### 1. 修改代码中的连接信息

编辑 `example_get_jobs.go`，修改以下部分：

```go
jenkinsURL := "http://jenkins.example.com"  // 改为你的 Jenkins URL
username := "your_username"                  // 改为你的用户名
password := "your_password"                  // 改为你的密码
```

### 2. 运行

```bash
cd /Users/masheilapincamamaril/Desktop/jenkins_export/jenkins_exporter
go run example_get_jobs.go
```

### 3. 或者使用测试脚本（不需要修改代码）

```bash
go run test_job_sdk.go \
  -url http://dc-tw-ebpay-pro-jenkins.zfit999.com \
  -user your_username \
  -pass your_password \
  -folder prod-gray-ebpay \
  -recursive
```

## 常见问题

### 问题1: 找不到 gojenkins 包
```bash
go get github.com/bndr/gojenkins
```

### 问题2: 连接超时
增加超时时间：
```go
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
```

### 问题3: 认证失败
检查用户名和密码是否正确，确保有访问权限。
