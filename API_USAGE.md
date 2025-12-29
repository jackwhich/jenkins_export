# Jenkins API 使用说明

## 数据获取方式

本 exporter 使用 **Jenkins REST API JSON 方式**获取数据，不是使用 Jenkins SDK。

## API 调用流程

### 1. Job Discovery（低频，5分钟）

**获取 job 列表：**
```
GET /api/json?depth=1                    # 获取根目录
GET /job/{folder}/api/json?depth=1       # 获取文件夹内容
GET /job/{folder}/job/{job}/api/json     # 获取 job 详情
```

**存储到 SQLite：**
- 使用 `job.Path`（即 `fullName`）作为 job 名称
- 例如：`"folder/job"` 或 `"folder/subfolder/job"`

### 2. Build Collector（高频，15秒）

**获取构建信息：**
```
GET /job/{folder}/job/{job}/api/json     # 获取 job 信息（包含 lastCompletedBuild）
GET {build.URL}/api/json                 # 获取构建详情
```

**路径转换：**
- 从 SQLite 读取 job 名称（例如 `"folder/job"`）
- 转换为 API 路径：`/job/folder/job/api/json`

## 可能的问题

### 问题 1: 路径转换错误

如果 job 名称包含特殊字符或格式不正确，路径转换可能失败。

**当前实现：**
```go
// jobName: "folder/job"
pathParts := strings.Split(jobName, "/")
apiPath := ""
for _, part := range pathParts {
    if part != "" {
        apiPath += "/job/" + part
    }
}
// 结果: "/job/folder/job"
```

### 问题 2: job.Path 格式不一致

如果 Jenkins 返回的 `fullName` 格式与预期不符，可能导致路径构建错误。

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

