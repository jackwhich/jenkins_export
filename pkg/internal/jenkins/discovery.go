package jenkins

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/promhippie/jenkins_exporter/pkg/internal/storage"
)

// convertJobPathForSDK converts job path from "folder/job" to "folder/job/job" format
// that gojenkins SDK expects.
// Example: "uat/pre-wallet-server" -> "uat/job/pre-wallet-server"
// Example: "folder/subfolder/job" -> "folder/job/subfolder/job/job"
func convertJobPathForSDK(fullName string) string {
	// 如果路径包含 "/"，说明是文件夹下的 job
	// gojenkins SDK 需要 "folder/job/job" 格式，而不是 "folder/job"
	if strings.Contains(fullName, "/") {
		parts := strings.Split(fullName, "/")
		// 将每个部分之间插入 "job"
		// 例如: ["uat", "pre-wallet-server"] -> "uat/job/pre-wallet-server"
		// 例如: ["folder", "subfolder", "job"] -> "folder/job/subfolder/job/job"
		result := parts[0]
		for i := 1; i < len(parts); i++ {
			result += "/job/" + parts[i]
		}
		return result
	}
	// 如果是顶层 job，直接返回
	return fullName
}

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
	logger.Info("开始同步 Job 列表",
		"指定文件夹", folders,
		"说明", "正在从 Jenkins 获取 job 列表并同步到 SQLite 数据库",
	)

	// 初始化 SDK（如果尚未初始化）
	logger.Info("正在初始化 Jenkins SDK...")
	if err := client.InitSDK(logger); err != nil {
		return fmt.Errorf("failed to initialize SDK: %w", err)
	}
	logger.Info("Jenkins SDK 初始化成功")

	// 使用 SDK 递归获取所有 job（包括文件夹下的所有 job）
	// 返回 job 列表和路径映射（因为 gojenkins.Job.GetName() 可能只返回相对名称）
	logger.Info("正在从 Jenkins 获取 job 列表（递归获取所有文件夹下的 job）...")
	sdkJobs, jobPathMap, err := client.SDK.GetAllJobsRecursive(ctx, folders, logger)
	if err != nil {
		return fmt.Errorf("failed to get jobs from Jenkins SDK: %w", err)
	}
	
	logger.Info("从 Jenkins 获取到 job 列表",
		"原始 job 数量", len(sdkJobs),
		"说明", "正在过滤文件夹和排除的文件夹...",
	)

	// 提取 job 名称（使用路径映射获取完整路径），并过滤掉排除的文件夹
	excludedFolders := map[string]bool{
		"prod-ebpay-new":  true,
		"pre-ebpay-new":   true,
		"prod-gray-ebpay":  true,
	}
	
	jobNames := make([]string, 0, len(sdkJobs))
	excludedCount := 0
	folderCount := 0
	totalJobs := len(sdkJobs)
	
	logger.Info("开始处理 job 列表",
		"总 job 数量", totalJobs,
		"说明", "正在逐个处理每个 job，过滤文件夹和排除的文件夹...",
	)
	
	processedCount := 0
	validCount := 0
	progressInterval := 50 // 每处理 50 个 job 输出一次进度
	
	for i, job := range sdkJobs {
		processedCount = i + 1
		// 优先使用路径映射中的完整路径，如果没有则使用 GetName()
		fullName := jobPathMap[job]
		if fullName == "" {
			// 如果路径映射中没有，尝试使用 GetName()
			fullName = job.GetName()
		}
		
		if fullName == "" {
			logger.Debug("跳过空名称的 job",
				"job_info", fmt.Sprintf("%+v", job),
			)
			continue
		}
		
		// 再次验证：确保不是文件夹类型的 job
		// 虽然 GetAllJobsRecursive 已经过滤了，但为了安全起见，这里再次检查
		isFolder := false
		if job.Raw != nil {
			jobClass := job.Raw.Class
			if jobClass != "" {
				if strings.Contains(jobClass, "Folder") || 
				   strings.Contains(jobClass, "folder") ||
				   strings.Contains(jobClass, "com.cloudbees.hudson.plugins.folder") {
					isFolder = true
				}
			}
		}
		
		// 如果 Raw 为空或 Class 未设置，尝试通过 GetInnerJobs 来判断
		// 注意：这会产生额外的 API 调用，但可以更准确地识别文件夹
		if !isFolder && (job.Raw == nil || job.Raw.Class == "") {
			// 创建子 context，避免超时影响整体
			checkCtx, checkCancel := context.WithTimeout(ctx, 5*time.Second)
			subJobs, err := job.GetInnerJobs(checkCtx)
			checkCancel()
			
			if err == nil {
				// 能成功调用 GetInnerJobs，说明是文件夹
				isFolder = true
				logger.Debug("在 Discovery 阶段检测到文件夹类型，跳过",
					"job_name", fullName,
					"子项数量", len(subJobs),
				)
			}
		}
		
		if isFolder {
			folderCount++
			logger.Debug("跳过文件夹类型的 job（在 Discovery 阶段）",
				"job_name", fullName,
			)
			continue
		}
		
		// 记录 job 的完整路径信息（用于调试）
		source := "GetName()"
		if jobPathMap[job] != "" {
			source = "路径映射"
		}
		logger.Debug("获取到构建 job 完整路径",
			"full_name", fullName,
			"来源", source,
			"说明", "将存储到 SQLite。如果是文件夹下的 job，应该是完整路径 folder/job",
		)
		
		// 检查是否是排除的文件夹下的 job
		parts := strings.Split(fullName, "/")
		if len(parts) > 0 {
			topLevelFolder := parts[0]
			if excludedFolders[topLevelFolder] {
				excludedCount++
				logger.Debug("过滤掉排除的文件夹下的 job",
					"job_name", fullName,
					"顶层文件夹", topLevelFolder,
				)
				continue
			}
		}
		
		// 将路径转换为 SDK 格式（folder/job -> folder/job/job）
		// 这样存储到数据库后，采集时可以直接使用，不需要再次转换
		sdkPath := convertJobPathForSDK(fullName)
		logger.Debug("转换 job 路径为 SDK 格式",
			"原始路径", fullName,
			"SDK 路径", sdkPath,
			"说明", "存储到数据库的路径已经是 SDK 格式，采集时可直接使用",
		)
		
		jobNames = append(jobNames, sdkPath)
		validCount++
		
		// 每处理一定数量的 job 输出一次进度
		if processedCount%progressInterval == 0 || processedCount == totalJobs {
			logger.Info("处理进度",
				"已处理", processedCount,
				"总数", totalJobs,
				"有效 job", validCount,
				"过滤掉的文件夹", folderCount,
				"过滤掉的排除文件夹", excludedCount,
				"进度", fmt.Sprintf("%.1f%%", float64(processedCount)*100.0/float64(totalJobs)),
			)
		}
	}
	
	if folderCount > 0 {
		logger.Info("过滤掉文件夹类型的 job",
			"文件夹数量", folderCount,
		)
	}
	
	if excludedCount > 0 {
		logger.Info("过滤掉排除的文件夹下的 job",
			"排除数量", excludedCount,
			"剩余数量", len(jobNames),
		)
	}

	if len(jobNames) == 0 {
		logger.Warn("从 Jenkins 获取到的 job 列表为空",
			"指定文件夹", folders,
			"原始 job 数量", len(sdkJobs),
			"过滤掉的文件夹数量", folderCount,
			"过滤掉的排除文件夹数量", excludedCount,
			"建议", "请检查 Jenkins 连接、文件夹配置或排除文件夹配置",
		)
		return nil
	}

	logger.Info("处理完成，准备同步到 SQLite 数据库",
		"已处理总数", processedCount,
		"有效 job 数量", len(jobNames),
		"过滤掉的文件夹", folderCount,
		"过滤掉的排除文件夹", excludedCount,
		"指定文件夹", folders,
		"说明", "正在将 job 列表同步到数据库（新增、更新或软删除 job 记录）...",
	)

	// 同步到 SQLite
	if err := repo.SyncJobs(jobNames); err != nil {
		return fmt.Errorf("failed to sync jobs to SQLite: %w", err)
	}

	// 获取同步后的统计信息（从数据库读取实际数量）
	enabledJobs, err := repo.ListEnabledJobs()
	enabledCount := 0
	if err == nil {
		enabledCount = len(enabledJobs)
	}

	logger.Info("✅ Job 列表同步完成",
		"统计信息", map[string]interface{}{
			"从 Jenkins 获取":        len(sdkJobs),
			"已处理总数":            processedCount,
			"有效 job 数量":         len(jobNames),
			"数据库中的启用 job 数量": enabledCount,
			"过滤掉的文件夹":         folderCount,
			"过滤掉的排除文件夹":       excludedCount,
		},
		"指定文件夹", folders,
		"说明", fmt.Sprintf("数据库已更新，共 %d 个 job 已同步完成，Collector 可以开始采集这些 job 的构建信息", enabledCount),
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

