# Go SDK 获取 Jenkins Job 详细代码示例

## 核心代码片段

### 1. 初始化 Jenkins 客户端

```go
import (
    "context"
    "time"
    "github.com/bndr/gojenkins"
)

// 创建客户端
jenkins := gojenkins.CreateJenkins(nil, "http://jenkins.example.com", "username", "password")

// 初始化连接
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

_, err := jenkins.Init(ctx)
if err != nil {
    log.Fatal(err)
}
```

### 2. 获取所有顶层 job

```go
allJobs, err := jenkins.GetAllJobs(ctx)
if err != nil {
    log.Fatal(err)
}

for _, job := range allJobs {
    fmt.Printf("Job: %s\n", job.GetName())
}
```

### 3. 获取指定文件夹下的所有 job（递归）

```go
// 获取文件夹
folderJob, err := jenkins.GetJob(ctx, "prod-gray-ebpay")
if err != nil {
    log.Fatal(err)
}

// 检查是否是文件夹
if job.Raw != nil && strings.Contains(job.Raw.Class, "Folder") {
    // 获取文件夹下的子项
    subJobs, err := folderJob.GetInnerJobs(ctx)
    if err != nil {
        log.Fatal(err)
    }
    
    // 递归处理每个子项
    for _, subJob := range subJobs {
        // 继续递归...
    }
}
```

### 4. 获取指定 job 的详细信息

```go
job, err := jenkins.GetJob(ctx, "prod-gray-ebpay/gray-prod-mkt-thirdpart-api")
if err != nil {
    log.Fatal(err)
}

// 获取 job 基本信息
fmt.Printf("名称: %s\n", job.GetName())
fmt.Printf("类型: %s\n", job.Raw.Class)
fmt.Printf("URL: %s\n", job.Raw.URL)

// 获取最后一次构建
lastBuild, err := job.GetLastCompletedBuild(ctx)
if err == nil && lastBuild != nil {
    fmt.Printf("构建编号: #%d\n", lastBuild.GetBuildNumber())
    fmt.Printf("构建结果: %s\n", lastBuild.GetResult())
    fmt.Printf("构建时间: %v\n", lastBuild.GetTimestamp())
    fmt.Printf("构建时长: %d ms\n", lastBuild.GetDuration())
    
    // 获取构建参数
    params := lastBuild.GetParameters()
    for _, param := range params {
        fmt.Printf("参数: %s = %v\n", param.Name, param.Value)
    }
}
```

### 5. 递归获取文件夹下的所有 job（完整函数）

```go
func getAllJobsRecursive(ctx context.Context, job *gojenkins.Job) []*gojenkins.Job {
    allJobs := make([]*gojenkins.Job, 0)
    
    // 检查是否是文件夹
    isFolder := false
    if job.Raw != nil {
        jobClass := job.Raw.Class
        if jobClass != "" && strings.Contains(jobClass, "Folder") {
            isFolder = true
        }
    }
    
    if isFolder {
        // 获取文件夹下的子项
        if job.Raw != nil && job.Raw.Jobs != nil {
            subJobs, err := job.GetInnerJobs(ctx)
            if err != nil {
                return allJobs
            }
            
            // 递归处理每个子项
            for _, subJob := range subJobs {
                jobs := getAllJobsRecursive(ctx, subJob)
                allJobs = append(allJobs, jobs...)
            }
        }
    } else {
        // 实际的构建 job，直接添加
        allJobs = append(allJobs, job)
    }
    
    return allJobs
}
```

### 6. 检查 job 是否是文件夹

```go
func isFolder(job *gojenkins.Job) bool {
    if job.Raw != nil {
        jobClass := job.Raw.Class
        if jobClass != "" && strings.Contains(jobClass, "Folder") {
            return true
        }
    }
    return false
}
```

## 完整示例

查看 `example_get_jobs.go` 和 `example_get_jobs_simple.go` 获取完整可运行的示例代码。
