package jenkins

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/promhippie/jenkins_exporter/pkg/internal/storage"
)

// StartDiscovery starts the job discovery process that periodically syncs job list from Jenkins to SQLite.
// It runs at the specified interval (recommended: 5-10 minutes).
func StartDiscovery(ctx context.Context, client *Client, repo *storage.JobRepo, interval time.Duration, folders []string, logger *slog.Logger) error {
	logger = logger.With("component", "discovery")

	logger.Info("启动 Job Discovery",
		"同步间隔", interval,
		"指定文件夹", folders,
	)

	// 立即执行一次同步
	if err := syncJobsOnce(ctx, client, repo, folders, logger); err != nil {
		logger.Warn("首次同步失败，将在下一个周期重试",
			"错误", err,
		)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Job Discovery 已停止",
				"原因", ctx.Err(),
			)
			return ctx.Err()
		case <-ticker.C:
			if err := syncJobsOnce(ctx, client, repo, folders, logger); err != nil {
				logger.Warn("Job 列表同步失败，将在下一个周期重试",
					"错误", err,
				)
				// 继续运行，不中断服务
			}
		}
	}
}

// syncJobsOnce performs a single synchronization of jobs from Jenkins to SQLite.
func syncJobsOnce(ctx context.Context, client *Client, repo *storage.JobRepo, folders []string, logger *slog.Logger) error {
	logger.Debug("开始同步 Job 列表")

	// 从 Jenkins 获取所有 job 列表
	jobClient := client.Job
	jobs, err := jobClient.All(ctx, folders)
	if err != nil {
		return fmt.Errorf("failed to get jobs from Jenkins: %w", err)
	}

	// 提取 job 名称（使用 Path 作为全名）
	jobNames := make([]string, 0, len(jobs))
	for _, job := range jobs {
		if job.Path != "" {
			jobNames = append(jobNames, job.Path)
		}
	}

	if len(jobNames) == 0 {
		logger.Warn("从 Jenkins 获取到的 job 列表为空")
		return nil
	}

	// 同步到 SQLite
	if err := repo.SyncJobs(jobNames); err != nil {
		return fmt.Errorf("failed to sync jobs to SQLite: %w", err)
	}

	logger.Info("Job 列表同步完成",
		"job 数量", len(jobNames),
	)

	return nil
}

// GetJobNamesFromFolders extracts job names from a folder string (comma-separated).
func GetJobNamesFromFolders(foldersStr string) []string {
	if foldersStr == "" {
		return nil
	}

	parts := strings.Split(foldersStr, ",")
	folders := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			folders = append(folders, trimmed)
		}
	}

	return folders
}

