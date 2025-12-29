# Jenkins API 使用说明

## 数据获取方式

本 exporter 使用 **gojenkins SDK** 获取数据，而不是直接调用 REST API。这样可以确保数据获取的准确性和可靠性。

## SDK 使用流程

### 1. Job Discovery（低频，5分钟）

**使用 gojenkins SDK 获取 job 列表：**
```go
jenkins.GetAllJobs(ctx)  // 递归获取所有 job
job.GetName()            // 获取 job 完整路径（fullName）
```

**存储到 SQLite：**
- 使用 `job.GetName()` 获取完整路径作为 job 名称
- 例如：`"folder/job"` 或 `"folder/subfolder/job"`

### 2. Build Collector（高频，15秒）

**使用 gojenkins SDK 获取构建信息：**
```go
jenkins.GetJob(ctx, fullName)              // 根据完整路径获取 job
job.GetLastCompletedBuild(ctx)             // 获取最后一次完成的构建
build.GetParameters()                      // 获取构建参数
build.GetResult()                          // 获取构建结果
```

**优势：**
- SDK 自动处理路径编码和 URL 构建
- 更可靠的数据解析
- 更好的错误处理

## SDK 优势

### 1. 自动路径处理

gojenkins SDK 自动处理 job 路径的编码和构建，无需手动转换：
```go
// SDK 自动处理
job, err := jenkins.GetJob(ctx, "folder/job")
// 内部自动处理 URL 编码和路径构建
```

### 2. 更准确的数据解析

SDK 提供了类型安全的方法，避免手动解析 JSON：
```go
build.GetResult()        // 直接返回字符串结果
build.GetBuildNumber()   // 直接返回构建编号
build.GetParameters()   // 直接返回参数列表
```

### 3. 更好的错误处理

SDK 提供了更详细的错误信息，便于调试和问题定位。

## 调试方法

### 1. 查看实际存储的 job 名称

```bash
sqlite3 /var/lib/jenkins_exporter/jobs.db "SELECT job_name FROM jobs LIMIT 10;"
```

### 2. 查看日志

检查 exporter 日志中的错误信息：
```bash
journalctl -u jenkins-exporter -f | grep -i error
```

### 3. 手动测试 API

```bash
# 测试获取 job 信息
curl -u username:password \
  "http://jenkins.example.com/job/folder/job/job_name/api/json"

# 查看返回的 fullName
curl -u username:password \
  "http://jenkins.example.com/job/folder/job/job_name/api/json" | jq '.fullName'
```

## 改进建议

### 方案 1: 存储 job URL（推荐）

在 Discovery 阶段同时存储 job.URL，这样在 Collector 阶段可以直接使用：

```go
// 修改表结构，添加 job_url 字段
ALTER TABLE jobs ADD COLUMN job_url TEXT;

// 在 Discovery 阶段存储 URL
INSERT INTO jobs(job_name, job_url, ...) VALUES (?, ?, ...)

// 在 Collector 阶段直接使用 URL
GET {job_url}/api/json
```

### 方案 2: 改进路径构建

使用更可靠的方式构建路径，处理特殊字符：

```go
func buildJobPath(jobName string) string {
    // 处理 URL 编码
    parts := strings.Split(jobName, "/")
    encodedParts := make([]string, 0, len(parts))
    for _, part := range parts {
        if part != "" {
            encodedParts = append(encodedParts, url.PathEscape(part))
        }
    }
    return "/job/" + strings.Join(encodedParts, "/job/")
}
```

## 当前实现总结

- ✅ 使用 REST API JSON 方式（不是 SDK）
- ✅ Discovery: 调用 `/api/json?depth=1` 递归获取 job 列表
- ✅ Collector: 调用 `/job/{path}/api/json` 获取 job 信息
- ⚠️ 路径转换可能有问题（需要根据实际情况调整）

