# Jenkins Exporter 代码分析与指标文档

## 目录
1. [不需要的代码](#不需要的代码)
2. [Prometheus 指标说明](#prometheus-指标说明)
3. [指标生成流程](#指标生成流程)

---

## 不需要的代码

### 1. `pkg/internal/jenkins/dumper.go` - 整个文件可以删除

**文件位置**: `pkg/internal/jenkins/dumper.go`

**包含内容**:
- `Dumper` 接口定义
- `DiscardDumper()` 函数
- `StandardDumper()` 函数
- `discardDumper` 结构体及其方法
- `standardDumper` 结构体及其方法

**删除原因**:
- 虽然 `Dumper` 接口在 `client.go` 中被引用，但从未被实际使用
- `WithHTTPDumper()` 函数从未被调用
- `httpDumper` 字段在 `NewClient` 中被注释掉（第77行）
- `httpDumper` 始终为 `nil`，相关检查代码（第134-135行和148-149行）永远不会执行

### 2. `pkg/internal/jenkins/client.go` 中的未使用代码

**文件位置**: `pkg/internal/jenkins/client.go`

**需要删除的内容**:

#### 2.1 `WithHTTPClient()` 函数（第35-40行）
```go
func WithHTTPClient(value *http.Client) ClientOption {
    return func(client *Client) error {
        client.httpClient = value
        return nil
    }
}
```
**删除原因**: 从未被调用

#### 2.2 `WithHTTPDumper()` 函数（第43-48行）
```go
func WithHTTPDumper(value Dumper) ClientOption {
    return func(client *Client) error {
        client.httpDumper = value
        return nil
    }
}
```
**删除原因**: 从未被调用

#### 2.3 `httpDumper` 字段（第23行）
```go
httpDumper Dumper
```
**删除原因**: 始终为 `nil`，从未被设置

#### 2.4 `httpDumper` 相关的检查逻辑
- 第134-135行: `DumpRequest` 调用
- 第148-149行: `DumpResponse` 调用

**删除原因**: 死代码，永远不会执行

### 3. `pkg/command/health.go` 中的潜在重复

**文件位置**: `pkg/command/health.go`

**说明**: `HealthFlags()` 函数只定义了一个 `web.address` 标志，该标志在 `RootFlags()` 中已经定义。如果 `Health` 命令需要独立的标志配置，可以保留；否则可以考虑简化。

---

## Prometheus 指标说明

### 指标命名空间

所有指标使用 `jenkins` 作为命名空间。

### 指标分类

#### 1. 系统指标（自动注册）

这些指标由 Prometheus 客户端库自动提供：

##### 1.1 进程指标 (`jenkins_process_*`)
- **来源**: `collectors.NewProcessCollector()`
- **位置**: `pkg/action/metrics.go:39-41`
- **说明**: 提供进程相关的指标，如 CPU 使用率、内存使用、文件描述符等

##### 1.2 Go 运行时指标 (`jenkins_go_*`)
- **来源**: `collectors.NewGoCollector()`
- **位置**: `pkg/action/metrics.go:43`
- **说明**: 提供 Go 运行时相关的指标，如 goroutine 数量、GC 统计等

##### 1.3 构建信息指标
- **指标名**: `jenkins_build_info`
- **类型**: Gauge
- **标签**: `version`, `revision`, `goversion`
- **值**: 固定为 `1`
- **来源**: `pkg/version/collector.go`
- **说明**: 记录构建时的版本信息

#### 2. 请求性能指标

##### 2.1 `jenkins_request_duration_seconds`
- **类型**: Histogram
- **标签**: `collector` (收集器名称，如 "job")
- **说明**: 记录每个收集器请求 Jenkins API 的延迟时间（秒）
- **桶**: `[0.001, 0.01, 0.1, 0.5, 1.0, 2.0, 5.0, 10.0]`
- **位置**: `pkg/action/metrics.go:18-26`
- **更新时机**: 每次调用 `JobCollector.Collect()` 时记录请求耗时

##### 2.2 `jenkins_request_failures_total`
- **类型**: Counter
- **标签**: `collector` (收集器名称，如 "job")
- **说明**: 记录每个收集器请求 Jenkins API 失败的总次数
- **位置**: `pkg/action/metrics.go:28-35`
- **更新时机**: 
  - 获取作业列表失败时
  - 获取构建详情失败时

#### 3. Jenkins 作业指标

所有作业指标都包含以下标签：
- `name`: 作业显示名称
- `path`: 作业完整路径
- `class`: 作业类型（如 `hudson.model.FreeStyleProject`）

##### 3.1 `jenkins_job_disabled`
- **类型**: Gauge
- **值**: `1` 表示作业已禁用，`0` 表示未禁用
- **位置**: `pkg/exporter/job.go:51-56, 210-219`
- **数据来源**: `job.Disabled` 字段

##### 3.2 `jenkins_job_buildable`
- **类型**: Gauge
- **值**: `1` 表示作业可构建，`0` 表示不可构建
- **位置**: `pkg/exporter/job.go:57-62, 221-230`
- **数据来源**: `job.Buildable` 字段

##### 3.3 `jenkins_job_color`
- **类型**: Gauge
- **值**: 颜色代码（见下表）
- **位置**: `pkg/exporter/job.go:63-68, 232-237, 343-376`
- **数据来源**: `job.Color` 字段

**颜色值映射**:
| 颜色 | 值 |
|------|-----|
| `blue` | 1.0 |
| `blue_anime` | 1.5 |
| `red` | 2.0 |
| `red_anime` | 2.5 |
| `yellow` | 3.0 |
| `yellow_anime` | 3.5 |
| `notbuilt` | 4.0 |
| `notbuilt_anime` | 4.5 |
| `disabled` | 5.0 |
| `disabled_anime` | 5.5 |
| `aborted` | 6.0 |
| `aborted_anime` | 6.5 |
| `grey` | 7.0 |
| `grey_anime` | 7.5 |
| 其他 | 0.0 |

##### 3.4 `jenkins_job_last_build`
- **类型**: Gauge
- **值**: 最后一次构建的构建号
- **位置**: `pkg/exporter/job.go:69-74, 239-245`
- **数据来源**: `job.LastBuild.Number`
- **条件**: 仅当 `job.LastBuild != nil` 时导出

##### 3.5 `jenkins_job_last_completed_build`
- **类型**: Gauge
- **值**: 最后一次完成的构建的构建号
- **位置**: `pkg/exporter/job.go:75-80, 280-287`
- **数据来源**: `job.LastCompletedBuild.Number`
- **条件**: 仅当 `job.LastCompletedBuild != nil` 时导出

##### 3.6 `jenkins_job_last_failed_build`
- **类型**: Gauge
- **值**: 最后一次失败的构建的构建号
- **位置**: `pkg/exporter/job.go:81-86, 289-296`
- **数据来源**: `job.LastFailedBuild.Number`
- **条件**: 仅当 `job.LastFailedBuild != nil` 时导出

##### 3.7 `jenkins_job_last_stable_build`
- **类型**: Gauge
- **值**: 最后一次稳定的构建的构建号
- **位置**: `pkg/exporter/job.go:87-92, 298-305`
- **数据来源**: `job.LastStableBuild.Number`
- **条件**: 仅当 `job.LastStableBuild != nil` 时导出

##### 3.8 `jenkins_job_last_successful_build`
- **类型**: Gauge
- **值**: 最后一次成功的构建的构建号
- **位置**: `pkg/exporter/job.go:93-98, 307-314`
- **数据来源**: `job.LastSuccessfulBuild.Number`
- **条件**: 仅当 `job.LastSuccessfulBuild != nil` 时导出

##### 3.9 `jenkins_job_last_unstable_build`
- **类型**: Gauge
- **值**: 最后一次不稳定的构建的构建号
- **位置**: `pkg/exporter/job.go:99-104, 316-323`
- **数据来源**: `job.LastUnstableBuild.Number`
- **条件**: 仅当 `job.LastUnstableBuild != nil` 时导出

##### 3.10 `jenkins_job_last_unsuccessful_build`
- **类型**: Gauge
- **值**: 最后一次不成功的构建的构建号
- **位置**: `pkg/exporter/job.go:105-110, 325-332`
- **数据来源**: `job.LastUnsuccessfulBuild.Number`
- **条件**: 仅当 `job.LastUnsuccessfulBuild != nil` 时导出

##### 3.11 `jenkins_job_next_build_number`
- **类型**: Gauge
- **值**: 下一个构建号
- **位置**: `pkg/exporter/job.go:111-116, 334-340`
- **数据来源**: `job.NextBuildNumber`
- **说明**: 总是导出（即使为 0）

##### 3.12 `jenkins_job_duration`
- **类型**: Gauge
- **值**: 最后一次构建的持续时间（毫秒）
- **位置**: `pkg/exporter/job.go:117-122, 257-262`
- **数据来源**: `build.Duration`
- **条件**: 仅当成功获取最后一次构建详情时导出

##### 3.13 `jenkins_job_start_time`
- **类型**: Gauge
- **值**: 最后一次构建的开始时间（Unix 时间戳）
- **位置**: `pkg/exporter/job.go:123-128, 264-269`
- **数据来源**: `build.Timestamp`
- **条件**: 仅当成功获取最后一次构建详情时导出

##### 3.14 `jenkins_job_end_time`
- **类型**: Gauge
- **值**: 最后一次构建的结束时间（Unix 时间戳）
- **位置**: `pkg/exporter/job.go:129-134, 271-276`
- **数据来源**: `build.Timestamp + build.Duration`
- **条件**: 仅当成功获取最后一次构建详情时导出

---

## 指标生成流程

### 1. 初始化阶段

#### 1.1 指标注册器创建
- **位置**: `pkg/action/metrics.go:12-15`
- **说明**: 创建独立的 Prometheus 注册器，使用 `jenkins` 命名空间

#### 1.2 自动注册系统指标
- **位置**: `pkg/action/metrics.go:38-44`
- **流程**:
  1. 注册进程收集器 (`NewProcessCollector`)
  2. 注册 Go 运行时收集器 (`NewGoCollector`)
  3. 注册版本信息收集器 (`version.Collector`)

#### 1.3 注册请求性能指标
- **位置**: `pkg/action/metrics.go:46-47`
- **流程**:
  1. 注册 `requestDuration` (Histogram)
  2. 注册 `requestFailures` (Counter)

### 2. 服务器启动阶段

#### 2.1 创建 Jenkins 客户端
- **位置**: `pkg/action/server.go:52-64`
- **说明**: 使用配置的用户名和密码创建 Jenkins API 客户端

#### 2.2 注册作业收集器
- **位置**: `pkg/action/server.go:136-146`
- **条件**: `cfg.Collector.Jobs == true`
- **流程**:
  1. 创建 `JobCollector` 实例
  2. 将收集器注册到全局注册器
  3. 传递 `requestFailures` 和 `requestDuration` 用于性能监控

#### 2.3 创建 HTTP 处理器
- **位置**: `pkg/action/server.go:148-153`
- **流程**:
  1. 使用 `promhttp.HandlerFor` 创建 Prometheus 指标处理器
  2. 配置错误日志记录器

#### 2.4 注册 HTTP 路由
- **位置**: `pkg/action/server.go:160-162`
- **说明**: 将指标处理器绑定到配置的路径（默认 `/metrics`）

### 3. 指标收集阶段

#### 3.1 触发收集
- **触发时机**: 当 Prometheus 服务器或用户访问 `/metrics` 端点时
- **位置**: `pkg/action/server.go:160-162`

#### 3.2 作业收集器执行
- **位置**: `pkg/exporter/job.go:177-341`
- **流程**:
  1. **创建超时上下文** (第178-179行)
     - 使用配置的 `cfg.Target.Timeout` 作为超时时间
   
  2. **获取作业列表** (第181-192行)
     - 调用 `c.client.Job.All(ctx)` 获取所有作业
     - 记录请求耗时到 `requestDuration` 指标
     - 如果失败，增加 `requestFailures` 计数器并返回
   
  3. **遍历每个作业** (第198-340行)
     - 为每个作业创建标签: `[name, path, class]`
     - 导出基础指标:
       - `jenkins_job_disabled`
       - `jenkins_job_buildable`
       - `jenkins_job_color`
       - `jenkins_job_next_build_number`
     
     - 如果存在 `LastBuild`:
       - 导出 `jenkins_job_last_build`
       - 调用 `c.client.Job.Build()` 获取构建详情
       - 如果成功，导出:
         - `jenkins_job_duration`
         - `jenkins_job_start_time`
         - `jenkins_job_end_time`
       - 如果失败，增加 `requestFailures` 计数器
     
     - 条件导出其他构建指标（如果对应的构建号不为 `nil`）:
       - `jenkins_job_last_completed_build`
       - `jenkins_job_last_failed_build`
       - `jenkins_job_last_stable_build`
       - `jenkins_job_last_successful_build`
       - `jenkins_job_last_unstable_build`
       - `jenkins_job_last_unsuccessful_build`

#### 3.3 指标聚合
- **位置**: Prometheus 客户端库内部
- **说明**: 所有收集器收集的指标被聚合到注册器中

#### 3.4 HTTP 响应
- **位置**: `pkg/action/server.go:160-162`
- **说明**: 将聚合后的指标以 Prometheus 文本格式返回给客户端

### 4. 数据流图

```
Prometheus Server / User
    ↓ (HTTP GET /metrics)
HTTP Handler (promhttp.HandlerFor)
    ↓
Prometheus Registry
    ↓
┌─────────────────────────────────────┐
│  Collectors (并发执行)                │
├─────────────────────────────────────┤
│  • ProcessCollector                 │
│  • GoCollector                      │
│  • VersionCollector                 │
│  • JobCollector                     │
│    └─> Jenkins API                  │
│        ├─> GET /api/json?depth=1    │
│        └─> GET /job/.../api/json    │
└─────────────────────────────────────┘
    ↓
指标聚合
    ↓
Prometheus 文本格式输出
```

### 5. 指标文档生成

#### 5.1 工具位置
- **文件**: `hack/generate-metrics-docs.go`
- **输出**: `docs/partials/metrics.md`

#### 5.2 生成流程
1. 创建 `JobCollector` 实例（使用 `nil` 客户端和配置）
2. 调用 `Metrics()` 方法获取所有指标描述符
3. 使用反射提取指标名称、帮助文本和标签
4. 添加系统指标（`request_duration_seconds`, `request_failures_total`）
5. 按名称排序
6. 生成 Markdown 格式文档

#### 5.3 运行方式
```bash
go run hack/generate-metrics-docs.go
```

---

## 总结

### 指标统计

- **系统指标**: 3 类（进程、Go 运行时、构建信息）
- **性能指标**: 2 个（请求延迟、请求失败）
- **作业指标**: 14 个
- **总计**: 19+ 个指标（系统指标包含多个子指标）

### 关键文件

- **指标定义**: `pkg/action/metrics.go`, `pkg/exporter/job.go`
- **指标收集**: `pkg/exporter/job.go:Collect()`
- **HTTP 暴露**: `pkg/action/server.go:handler()`
- **文档生成**: `hack/generate-metrics-docs.go`

---

## 代码适配性分析

### 当前代码对 Jenkins 环境的支持情况

#### ✅ 已支持的功能

1. **Jenkins 文件夹结构**
   - 完全支持递归遍历文件夹（如 `uat` 文件夹）
   - 代码位置: `pkg/internal/jenkins/job.go:62-105`
   - 可以正确处理嵌套文件夹中的作业

2. **作业基础信息**
   - 支持获取作业名称、路径、类型等基本信息
   - 支持获取各种构建号（最后构建、最后成功、最后失败等）

#### ❌ 不支持的功能

1. **构建参数（check_commitID, gitBranch）**
   - 当前 `Build` 结构体只包含 `Timestamp` 和 `Duration`
   - 没有获取构建参数的 API 调用
   - 指标标签中没有参数信息

2. **明确的构建状态指标**
   - 当前只有 `jenkins_job_color` 指标，通过颜色间接判断状态
   - 没有直接的构建状态指标（成功/失败/等待/Aborted）
   - 无法区分"正在运行"和"等待"状态

### 详细分析文档

请参考 `CODE_COMPATIBILITY_ANALYSIS.md` 获取：
- 详细的适配性分析
- 代码改进建议
- 实施优先级
- 测试建议

### 性能考虑

- 每个指标收集周期都会调用 Jenkins API
- 对于有 `LastBuild` 的作业，会额外调用一次 API 获取构建详情
- 使用超时机制防止长时间阻塞
- 使用 Histogram 和 Counter 监控收集器本身的性能

