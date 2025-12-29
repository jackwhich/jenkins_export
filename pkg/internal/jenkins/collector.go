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
	client        *Client
	repo          *storage.JobRepo
	logger        *slog.Logger
	buildResultGauge *prometheus.GaugeVec
	mu            sync.RWMutex
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

// collectOnce performs a single collection cycle.
func (c *BuildCollector) collectOnce(ctx context.Context) error {
	c.logger.Debug("开始采集构建结果")

	// 从 SQLite 读取 enabled=1 的 job
	jobs, err := c.repo.ListEnabledJobs()
	if err != nil {
		return fmt.Errorf("failed to list enabled jobs: %w", err)
	}

	if len(jobs) == 0 {
		c.logger.Debug("没有启用的 job 需要采集")
		return nil
	}

	c.logger.Debug("开始处理 job",
		"job 数量", len(jobs),
	)

	processedCount := 0
	updatedCount := 0
	errorCount := 0

	// 先清理所有旧指标
	c.mu.Lock()
	c.buildResultGauge.Reset()
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

		if err := c.processJob(ctx, job); err != nil {
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
		updatedCount++

		// 每处理 10 个 job 记录一次进度
		if processedCount%10 == 0 {
			c.logger.Debug("处理进度",
				"已处理", processedCount,
				"总数", len(jobs),
			)
		}
	}

	c.logger.Info("构建结果采集完成",
		"总 job 数", len(jobs),
		"已处理", processedCount,
		"已更新", updatedCount,
		"错误", errorCount,
	)

	return nil
}

// processJob processes a single job and updates metrics if needed.
func (c *BuildCollector) processJob(ctx context.Context, job storage.Job) error {
	// 初始化 SDK（如果尚未初始化）
	if err := c.client.InitSDK(c.logger); err != nil {
		return fmt.Errorf("failed to initialize SDK: %w", err)
	}

	// 检查 context 是否已取消
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// 使用 SDK 获取 job 的 lastCompletedBuild
	sdkBuild, buildNumber, err := c.client.SDK.GetLastCompletedBuild(ctx, job.JobName)
	if err != nil {
		// 如果是 context canceled，直接返回，不包装错误
		if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "context canceled") {
			return context.Canceled
		}
		return fmt.Errorf("failed to get last completed build: %w", err)
	}

	// 如果没有 completed build，跳过
	if sdkBuild == nil {
		c.logger.Debug("job 没有已完成的构建",
			"job_name", job.JobName,
		)
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
		return nil
	}

	// 增量处理：只有 build number 大于 last_seen_build 时才处理
	if buildNumber <= job.LastSeenBuild {
		c.logger.Debug("构建已处理过，跳过",
			"job_name", job.JobName,
			"build_number", buildNumber,
			"last_seen_build", job.LastSeenBuild,
		)
		return nil
	}

	// 检查 context 是否已取消
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// 获取构建详情（包括参数）
	buildDetails, err := c.client.SDK.GetBuildDetails(ctx, sdkBuild)
	if err != nil {
		// 如果是 context canceled，直接返回
		if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "context canceled") {
			return context.Canceled
		}
		c.logger.Warn("获取构建详情失败，使用基本信息",
			"job_name", job.JobName,
			"error", err,
		)
		// 即使获取详情失败，也使用基本信息
		buildDetails = &BuildDetails{
			Number:    buildNumber,
			Result:    sdkBuild.GetResult(),
			Building:  sdkBuild.IsRunning(ctx),
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

	// 更新指标
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

	// 更新 SQLite 的 last_seen_build
	if err := c.repo.UpdateLastSeen(job.JobName, buildNumber); err != nil {
		return fmt.Errorf("failed to update last_seen_build: %w", err)
	}

	c.logger.Debug("已处理 job 构建",
		"job_name", job.JobName,
		"build_number", buildNumber,
		"status", status,
		"使用 SDK", true,
	)

	return nil
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

