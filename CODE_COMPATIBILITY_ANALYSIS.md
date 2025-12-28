# Jenkins Exporter 代码适配性分析

## 概述

本文档分析当前代码对您的 Jenkins 环境的支持情况，包括：
1. 文件夹结构支持
2. 参数化构建支持
3. 构建状态监控支持

---

## 1. 当前代码支持情况

### ✅ 已支持的功能

#### 1.1 Jenkins 文件夹结构
- **支持情况**: ✅ **完全支持**
- **代码位置**: `pkg/internal/jenkins/job.go:62-105`
- **实现方式**: 
  - 通过 `recursiveFolders()` 函数递归遍历文件夹
  - 识别 `com.cloudbees.hudson.plugins.folder.Folder` 类型
  - 自动获取所有嵌套文件夹中的作业
- **您的环境**: 您的 Jenkins 有 `uat` 等文件夹，代码可以正确处理

#### 1.2 作业基础信息
- **支持情况**: ✅ **完全支持**
- **获取的信息**:
  - 作业名称 (`displayName`)
  - 作业路径 (`fullName`)
  - 作业类型 (`_class`)
  - 作业状态颜色 (`color`)
  - 各种构建号（最后构建、最后成功、最后失败等）

#### 1.3 构建历史信息
- **支持情况**: ✅ **部分支持**
- **当前获取**:
  - 最后构建号
  - 最后成功构建号
  - 最后失败构建号
  - 最后稳定构建号
  - 最后不稳定构建号
  - 最后未成功构建号
  - 构建持续时间
  - 构建开始/结束时间

---

## 2. 当前代码不支持的功能

### ❌ 不支持的功能

#### 2.1 构建参数（check_commitID, gitBranch）
- **支持情况**: ❌ **不支持**
- **问题分析**:
  - 当前 `Build` 结构体只包含 `Timestamp` 和 `Duration`
  - 没有获取构建参数的 API 调用
  - 指标标签中没有参数信息

**当前代码结构**:
```go
// pkg/internal/jenkins/types.go
type Build struct {
    Timestamp int64 `json:"timestamp"`
    Duration  int64 `json:"duration"`
    // ❌ 缺少: Result, Parameters 等字段
}
```

**Jenkins API 支持**:
- Jenkins API 可以通过 `/job/{jobName}/{buildNumber}/api/json` 获取构建详情
- 构建详情包含 `result` 字段（SUCCESS, FAILURE, ABORTED, UNSTABLE, null等）
- 构建详情包含 `actions` 数组，其中包含参数信息

#### 2.2 明确的构建状态指标
- **支持情况**: ❌ **不支持**
- **问题分析**:
  - 当前只有 `jenkins_job_color` 指标，通过颜色间接判断状态
  - 没有直接的构建状态指标（成功/失败/等待/Aborted）
  - 无法区分"正在运行"和"等待"状态

**当前状态判断方式**:
```go
// pkg/exporter/job.go:343-376
func colorToGauge(color string) float64 {
    // 通过颜色值判断，但不够明确
    case "blue": return 1.0        // 成功
    case "red": return 2.0         // 失败
    case "aborted": return 6.0    // 中止
    // ❌ 没有明确的"等待"状态
}
```

**您需要的状态**:
- ✅ 构建成功 (SUCCESS)
- ✅ 构建失败 (FAILURE)
- ✅ 等待 (QUEUED/IN_PROGRESS)
- ✅ Aborted (ABORTED)

---

## 3. 代码改进建议

### 3.1 扩展 Build 结构体

**需要添加的字段**:

```go
// pkg/internal/jenkins/types.go
type Build struct {
    Timestamp int64    `json:"timestamp"`
    Duration  int64    `json:"duration"`
    Result    string   `json:"result"`    // SUCCESS, FAILURE, ABORTED, UNSTABLE, null
    Building  bool     `json:"building"`  // 是否正在构建
    QueueId   int64    `json:"queueId"`   // 队列ID（如果在队列中）
    Actions   []Action `json:"actions"`   // 包含参数信息
}

type Action struct {
    Class            string                 `json:"_class"`
    Parameters       []Parameter            `json:"parameters,omitempty"`
    Causes           []Cause                `json:"causes,omitempty"`
}

type Parameter struct {
    Name  string      `json:"name"`
    Value interface{} `json:"value"`
}

type Cause struct {
    Class            string `json:"_class"`
    ShortDescription string `json:"shortDescription"`
}
```

### 3.2 添加新的指标

**需要添加的指标**:

```go
// pkg/exporter/job.go

// 构建状态指标
BuildStatus *prometheus.Desc  // 0=成功, 1=失败, 2=等待, 3=中止, 4=不稳定

// 构建参数指标（作为标签）
// 在现有指标中添加参数标签
```

**指标定义示例**:

```go
BuildStatus: prometheus.NewDesc(
    "jenkins_job_build_status",
    "Build status: 0=success, 1=failure, 2=queued, 3=aborted, 4=unstable, 5=in_progress",
    []string{"name", "path", "class", "check_commitID", "gitBranch"},
    nil,
),
```

### 3.3 修改 Build API 调用

**当前代码**:
```go
// pkg/internal/jenkins/job.go:29-43
func (c *JobClient) Build(ctx context.Context, build *BuildNumber) (Build, error) {
    result := Build{}
    req, err := c.client.NewRequest(ctx, "GET", fmt.Sprintf("%sapi/json", build.URL), nil)
    // ...
}
```

**改进建议**:
- 需要获取完整的构建信息，包括 `result`, `building`, `actions` 等
- 可能需要调用 `/api/json?tree=...` 来获取特定字段

### 3.4 修改指标收集逻辑

**需要修改的位置**: `pkg/exporter/job.go:Collect()`

**改进点**:
1. 获取构建详情时，提取参数信息
2. 根据构建状态设置状态指标
3. 在标签中包含参数值

**示例代码**:

```go
// 获取构建详情
build, err := c.client.Job.Build(ctx, job.LastBuild)
if err == nil {
    // 提取参数
    checkCommitID := extractParameter(build, "check_commitID")
    gitBranch := extractParameter(build, "gitBranch")
    
    // 扩展标签
    labels := []string{
        job.Name,
        job.Path,
        job.Class,
        checkCommitID,
        gitBranch,
    }
    
    // 设置状态指标
    status := buildStatusToValue(build.Result, build.Building)
    ch <- prometheus.MustNewConstMetric(
        c.BuildStatus,
        prometheus.GaugeValue,
        status,
        labels...,
    )
}
```

---

## 4. 状态映射方案

### 4.1 构建状态到数值的映射

| Jenkins 状态 | 数值 | 说明 |
|------------|------|------|
| SUCCESS | 0 | 构建成功 |
| FAILURE | 1 | 构建失败 |
| ABORTED | 2 | 构建中止 |
| UNSTABLE | 3 | 构建不稳定 |
| null + building=true | 4 | 正在构建中 |
| null + building=false + queueId>0 | 5 | 等待中（在队列中） |
| null + building=false + queueId=0 | 6 | 未构建 |

### 4.2 实现函数

```go
func buildStatusToValue(result string, building bool, queueId int64) float64 {
    if building {
        return 4.0 // 正在构建
    }
    
    if queueId > 0 {
        return 5.0 // 等待中
    }
    
    switch result {
    case "SUCCESS":
        return 0.0
    case "FAILURE":
        return 1.0
    case "ABORTED":
        return 2.0
    case "UNSTABLE":
        return 3.0
    default:
        return 6.0 // 未构建
    }
}
```

---

## 5. 参数提取方案

### 5.1 从 Actions 中提取参数

Jenkins API 返回的构建信息中，参数通常在 `actions` 数组的某个元素中：

```json
{
  "actions": [
    {
      "_class": "hudson.model.ParametersAction",
      "parameters": [
        {
          "name": "check_commitID",
          "value": "abc123"
        },
        {
          "name": "gitBranch",
          "value": "main"
        }
      ]
    }
  ]
}
```

### 5.2 提取函数

```go
func extractParameter(build Build, paramName string) string {
    for _, action := range build.Actions {
        if action.Class == "hudson.model.ParametersAction" {
            for _, param := range action.Parameters {
                if param.Name == paramName {
                    if str, ok := param.Value.(string); ok {
                        return str
                    }
                    return fmt.Sprintf("%v", param.Value)
                }
            }
        }
    }
    return "" // 未找到参数
}
```

---

## 6. 实施优先级

### 高优先级（必须实现）
1. ✅ **扩展 Build 结构体** - 添加 `result`, `building`, `actions` 字段
2. ✅ **添加构建状态指标** - `jenkins_job_build_status`
3. ✅ **修改 Build API 调用** - 获取完整构建信息

### 中优先级（建议实现）
4. ✅ **添加参数提取逻辑** - 提取 `check_commitID` 和 `gitBranch`
5. ✅ **在指标标签中包含参数** - 使指标可以按参数过滤

### 低优先级（可选）
6. ⚠️ **优化 API 调用** - 使用 `tree` 参数减少数据传输
7. ⚠️ **添加缓存机制** - 减少对 Jenkins API 的调用频率

---

## 7. 测试建议

### 7.1 测试场景

1. **参数化构建测试**
   - 创建带 `check_commitID` 和 `gitBranch` 参数的作业
   - 验证参数是否正确提取

2. **状态监控测试**
   - 测试成功构建
   - 测试失败构建
   - 测试中止构建
   - 测试正在运行的构建
   - 测试等待中的构建

3. **文件夹结构测试**
   - 验证嵌套文件夹中的作业是否正确获取
   - 验证路径标签是否正确

### 7.2 验证指标

使用 Prometheus 查询验证：

```promql
# 查看所有构建状态
jenkins_job_build_status

# 按参数过滤
jenkins_job_build_status{gitBranch="main", check_commitID="abc123"}

# 统计各状态数量
sum by (status) (jenkins_job_build_status)
```

---

## 8. 总结

### 当前代码适配情况

| 功能 | 支持情况 | 说明 |
|------|---------|------|
| 文件夹结构 | ✅ 完全支持 | 递归遍历文件夹 |
| 作业基础信息 | ✅ 完全支持 | 获取作业基本信息 |
| 构建历史 | ⚠️ 部分支持 | 缺少状态和参数 |
| 构建参数 | ❌ 不支持 | 需要扩展 |
| 构建状态 | ❌ 不支持 | 需要添加新指标 |

### 需要改进的地方

1. **数据结构扩展**: 扩展 `Build` 结构体以包含状态和参数
2. **API 调用增强**: 获取完整的构建信息
3. **指标添加**: 添加构建状态指标
4. **标签扩展**: 在指标标签中包含构建参数

### 建议

根据您的需求，建议优先实现：
1. 构建状态指标（成功/失败/等待/Aborted）
2. 构建参数提取（check_commitID, gitBranch）
3. 在指标标签中包含参数值

这样可以满足您对参数化构建监控的需求。

