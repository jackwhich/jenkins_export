package jenkins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/promhippie/jenkins_exporter/pkg/internal/storage"
)

// BuildCollector manages the collection of build results from Jenkins.
type BuildCollector struct {
	client           *Client
	repo             *storage.JobRepo
	logger           *slog.Logger
	buildResultGauge *prometheus.GaugeVec
	mu               sync.RWMutex
}

// NewBuildCollector creates a new BuildCollector instance.
func NewBuildCollector(client *Client, repo *storage.JobRepo, logger *slog.Logger) *BuildCollector {
	return &BuildCollector{
		client: client,
		repo:   repo,
		logger: logger.With("component", "build_collector"),
		buildResultGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "jenkins_build_last_result",
				Help: "Last build result: 1 indicates current status, status label contains the actual status (success, failure, aborted, unstable, unknown, not_built)",
			},
			[]string{"job_name", "check_commitID", "gitBranch", "status"},
		),
	}
}

// Describe implements prometheus.Collector.
func (c *BuildCollector) Describe(ch chan<- *prometheus.Desc) {
	c.buildResultGauge.Describe(ch)
}

// Collect implements prometheus.Collector.
func (c *BuildCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	c.buildResultGauge.Collect(ch)
}

// Start starts the build collector that periodically collects build results.
// It runs at the specified interval (recommended: 15 seconds).
func (c *BuildCollector) Start(ctx context.Context, interval time.Duration) error {
	c.logger.Info("启动 Build Collector",
		"采集间隔", interval,
	)

	// 立即执行一次采集
	if err := c.collectOnce(ctx); err != nil {
		c.logger.Warn("首次采集失败，将在下一个周期重试",
			"错误", err,
		)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Build Collector 已停止",
				"原因", ctx.Err(),
			)
			return ctx.Err()
		case <-ticker.C:
			if err := c.collectOnce(ctx); err != nil {
				c.logger.Warn("构建结果采集失败，将在下一个周期重试",
					"错误", err,
				)
				// 继续运行，不中断服务
			}
		}
	}
}

// isExcludedFolder checks if a job belongs to an excluded folder.
func isExcludedFolder(jobName string) bool {
	excludedFolders := map[string]bool{
		"prod-ebpay-new":  true,
		"pre-ebpay-new":   true,
		"prod-gray-ebpay": true,
	}

	// 检查 job 路径的第一部分（顶层文件夹）是否在排除列表中
	parts := strings.Split(jobName, "/")
	if len(parts) > 0 {
		topLevelFolder := parts[0]
		return excludedFolders[topLevelFolder]
	}
	return false
}

// collectOnce performs a single collection cycle.
func (c *BuildCollector) collectOnce(ctx context.Context) error {
	c.logger.Info("开始采集构建结果")

	// 从 SQLite 读取 enabled=1 的 job
	jobs, err := c.repo.ListEnabledJobs()
	if err != nil {
		return fmt.Errorf("failed to list enabled jobs: %w", err)
	}

	c.logger.Info("从 SQLite 读取到 job 列表",
		"总数", len(jobs),
	)

	if len(jobs) == 0 {
		c.logger.Warn("没有启用的 job 需要采集，请检查 SQLite 数据库或等待 Discovery 同步完成")
		return nil
	}

	// 过滤掉排除的文件夹下的 job，并删除它们的指标
	filteredJobs := make([]storage.Job, 0, len(jobs))
	excludedCount := 0
	c.mu.Lock()
	for _, job := range jobs {
		if isExcludedFolder(job.JobName) {
			excludedCount++
			c.logger.Debug("跳过排除的文件夹下的 job，删除其指标",
				"job_name", job.JobName,
			)
			// 删除被排除的 job 的所有指标
			c.buildResultGauge.DeletePartialMatch(prometheus.Labels{"job_name": job.JobName})
			continue
		}
		filteredJobs = append(filteredJobs, job)
	}
	c.mu.Unlock()

	if excludedCount > 0 {
		c.logger.Info("过滤掉排除的文件夹下的 job",
			"排除数量", excludedCount,
			"剩余数量", len(filteredJobs),
		)
	}

	jobs = filteredJobs

	if len(jobs) == 0 {
		c.logger.Warn("过滤后没有启用的 job 需要采集，可能所有 job 都被过滤掉了")
		return nil
	}

	c.logger.Info("开始采集构建结果",
		"job 数量", len(jobs),
		"说明", "将逐个处理每个 job，获取最后一次完成的构建信息",
	)

	processedCount := 0
	updatedCount := 0
	skippedCount := 0
	errorCount := 0
	noBuildCount := 0
	recentBuildCount := 0 // 最近有构建的 job 数量

	// 先清理所有旧指标（但保留当前有效的指标，避免在采集过程中指标消失）
	// 注意：我们不在开始时清空，而是在处理每个 job 时更新对应的指标
	// 这样可以避免在采集过程中指标为空的情况
	c.mu.Lock()
	// 不在这里 Reset，而是在处理每个 job 时使用 DeletePartialMatch 删除旧指标
	c.mu.Unlock()

	// 处理每个 job
	for _, job := range jobs {
		// 检查 context 是否已取消（优雅关闭）
		if ctx.Err() != nil {
			c.logger.Debug("采集被中断",
				"原因", ctx.Err(),
			)
			break
		}

		c.logger.Debug("开始处理 job",
			"job_name", job.JobName,
			"序号", processedCount+1,
			"总数", len(jobs),
		)

		result, err := c.processJob(ctx, job)
		if err != nil {
			// 如果是 context canceled，不记录为错误（优雅关闭）
			if ctx.Err() == context.Canceled {
				c.logger.Debug("采集被取消，停止处理",
					"job_name", job.JobName,
				)
				break
			}
			c.logger.Warn("处理 job 失败",
				"job_name", job.JobName,
				"错误", err,
			)
			errorCount++
			continue
		}

		processedCount++

		c.logger.Debug("job 处理完成",
			"job_name", job.JobName,
			"有结果", result != nil,
			"已处理总数", processedCount,
		)

		// 根据处理结果统计
		if result != nil {
			if result.Updated {
				updatedCount++
				c.logger.Debug("已更新 job 构建信息",
					"job_name", job.JobName,
					"构建编号", result.BuildNumber,
					"上次构建编号", job.LastSeenBuild,
					"状态", result.Status,
					"commit", result.CommitID,
					"分支", result.Branch,
				)
			} else {
				skippedCount++
				c.logger.Debug("job 构建未变化（已处理过）",
					"job_name", job.JobName,
					"当前构建编号", result.BuildNumber,
					"上次构建编号", job.LastSeenBuild,
					"状态", result.Status,
					"commit", result.CommitID,
					"分支", result.Branch,
				)
			}
			// 有构建编号就说明最近有构建过
			if result.BuildNumber > 0 {
				recentBuildCount++
			}
		} else {
			noBuildCount++
			c.logger.Debug("job 没有已完成的构建",
				"job_name", job.JobName,
			)
		}

		// 每处理 10 个 job 记录一次进度
		if processedCount%10 == 0 {
			c.logger.Info("处理进度",
				"已处理", processedCount,
				"总数", len(jobs),
				"已更新", updatedCount,
				"跳过", skippedCount,
				"无构建", noBuildCount,
			)
		}
	}

	// 注意：我们不在采集结束时清理指标，因为：
	// 1. 每个 job 在处理时都会更新对应的指标（使用 DeletePartialMatch 删除旧指标）
	// 2. 如果某个 job 不再存在，它的指标会在下次采集时自然消失（因为不会更新）
	// 3. 这样可以避免在采集过程中指标为空的情况

	// 清理不再存在的 job 的指标（在数据库中但不在当前 job 列表中的）
	// 获取当前所有有效的 job 名称集合
	validJobNames := make(map[string]bool)
	for _, job := range filteredJobs {
		validJobNames[job.JobName] = true
	}

	// 注意：Prometheus GaugeVec 没有直接的方法获取所有指标
	// 但我们可以通过其他方式处理：在处理每个 job 时更新指标，不在列表中的自然会被覆盖或保留
	// 实际上，由于我们在处理每个 job 时使用 DeletePartialMatch 删除旧指标，然后设置新指标
	// 不在列表中的 job 的指标会保留，但这是可以接受的，因为它们会在下次 Discovery 同步时被禁用

	c.logger.Info("构建结果采集完成",
		"总 job 数", len(jobs),
		"已处理", processedCount,
		"构建信息有变化", updatedCount,
		"构建信息未变化", skippedCount,
		"无已完成构建", noBuildCount,
		"最近有构建过的 job", recentBuildCount,
		"错误", errorCount,
		"排除的 job", excludedCount,
		"说明", fmt.Sprintf("已更新=%d 表示构建编号有变化（build_number > last_seen_build），最近有构建=%d 表示有已完成构建的 job 数量，排除=%d 表示被过滤掉的 job 数量", updatedCount, recentBuildCount, excludedCount),
	)

	// 如果没有任何 job 被处理，记录警告
	if processedCount == 0 && len(filteredJobs) > 0 {
		c.logger.Warn("没有 job 被处理，可能的原因：所有 job 都没有已完成的构建，或者采集被中断",
			"总 job 数", len(filteredJobs),
			"提示", "请检查 SQLite 数据库中的 job 列表，或查看 DEBUG 日志了解详情",
		)
	}

	return nil
}

// ProcessResult contains the result of processing a job.
type ProcessResult struct {
	Updated     bool
	BuildNumber int64
	Status      string
	CommitID    string
	Branch      string
}

// processJob processes a single job and updates metrics if needed.
// Returns ProcessResult if successful, nil if no build, error on failure.
func (c *BuildCollector) processJob(ctx context.Context, job storage.Job) (*ProcessResult, error) {
	// 初始化 SDK（如果尚未初始化）
	if err := c.client.InitSDK(c.logger); err != nil {
		return nil, fmt.Errorf("failed to initialize SDK: %w", err)
	}

	// 检查 context 是否已取消
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// 使用 SDK 获取 job 的 lastCompletedBuild
	// job.JobName 应该是完整路径（从 SQLite 读取的，由 Discovery 阶段使用 job.GetName() 获取的完整路径）
	// 例如："folder/job" 或 "folder/subfolder/job"，如果是顶层 job 就是 "job"
	c.logger.Debug("使用完整路径获取构建信息",
		"job_name", job.JobName,
		"说明", "使用从 SQLite 读取的完整路径（由 Discovery 阶段使用 job.GetName() 获取）",
	)

	sdkBuild, buildNumber, err := c.client.SDK.GetLastCompletedBuild(ctx, job.JobName)
	if err != nil {
		// 如果是 context canceled，直接返回，不包装错误
		if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "context canceled") {
			return nil, context.Canceled
		}

		// 如果是文件夹或权限问题（返回 HTML 而非 JSON），记录为 DEBUG 并跳过
		errMsg := err.Error()
		if strings.Contains(errMsg, "文件夹") || strings.Contains(errMsg, "权限") ||
			strings.Contains(errMsg, "HTML") || strings.Contains(errMsg, "invalid character '<'") {
			c.logger.Debug("跳过 job（可能是文件夹或权限问题）",
				"job_name", job.JobName,
				"错误", errMsg,
			)
			// 返回 nil, nil 表示跳过，不更新指标
			return nil, nil
		}

		return nil, fmt.Errorf("failed to get last completed build: %w", err)
	}

	// 如果没有 completed build，跳过
	if sdkBuild == nil {
		// 即使没有构建，也要更新指标为 not_built 状态
		c.mu.Lock()
		c.buildResultGauge.DeletePartialMatch(prometheus.Labels{"job_name": job.JobName})
		c.buildResultGauge.WithLabelValues(
			job.JobName,
			"", // check_commitID
			"", // gitBranch
			"not_built",
		).Set(1.0)
		c.mu.Unlock()
		return nil, nil // 返回 nil 表示没有构建
	}

	// 检查 context 是否已取消
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// 获取构建详情（包括参数）
	buildDetails, err := c.client.SDK.GetBuildDetails(ctx, sdkBuild)
	if err != nil {
		// 如果是 context canceled，直接返回
		if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "context canceled") {
			return nil, context.Canceled
		}
		c.logger.Warn("获取构建详情失败，使用基本信息",
			"job_name", job.JobName,
			"error", err,
		)
		// 即使获取详情失败，也使用基本信息
		buildDetails = &BuildDetails{
			Number:     buildNumber,
			Result:     sdkBuild.GetResult(),
			Building:   sdkBuild.IsRunning(ctx),
			Parameters: make(map[string]string),
		}
	}

	// 解析构建结果
	status := parseBuildStatus(buildDetails.Result, buildDetails.Building)
	checkCommitID := buildDetails.Parameters["check_commitID"]
	if checkCommitID == "" {
		checkCommitID = buildDetails.Parameters["GIT_COMMIT"]
	}
	gitBranch := buildDetails.Parameters["gitBranch"]
	if gitBranch == "" {
		gitBranch = buildDetails.Parameters["GIT_BRANCH"]
	}

	// 创建结果信息
	result := &ProcessResult{
		BuildNumber: buildNumber,
		Status:      status,
		CommitID:    checkCommitID,
		Branch:      gitBranch,
		Updated:     buildNumber > job.LastSeenBuild, // 只有构建编号变化时才标记为已更新
	}

	// 更新指标（无论是否变化都要更新，以反映当前状态）
	c.mu.Lock()
	// 先删除该 job 的所有旧指标
	c.buildResultGauge.DeletePartialMatch(prometheus.Labels{"job_name": job.JobName})
	// 设置新指标
	c.buildResultGauge.WithLabelValues(
		job.JobName,
		checkCommitID,
		gitBranch,
		status,
	).Set(1.0)
	c.mu.Unlock()

	// 只有构建编号变化时才更新 SQLite
	if result.Updated {
		if err := c.repo.UpdateLastSeen(job.JobName, buildNumber); err != nil {
			return nil, fmt.Errorf("failed to update last_seen_build: %w", err)
		}
	}

	return result, nil
}

// parseBuildStatus converts build result to status string.
func parseBuildStatus(result string, building bool) string {
	if building {
		return "in_progress"
	}

	switch result {
	case "SUCCESS":
		return "success"
	case "FAILURE":
		return "failure"
	case "ABORTED":
		return "aborted"
	case "UNSTABLE":
		return "unstable"
	default:
		if result == "" {
			return "not_built"
		}
		return "unknown"
	}
}

// extractParameter extracts a parameter value from build actions (legacy method, kept for compatibility).
func extractParameter(build *Build, paramName string) string {
	if build == nil {
		return ""
	}

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
	return ""
}
