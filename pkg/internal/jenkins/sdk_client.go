package jenkins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bndr/gojenkins"
)

// SDKClient wraps gojenkins SDK for better integration.
type SDKClient struct {
	jenkins *gojenkins.Jenkins
	logger  *slog.Logger
}

// NewSDKClient creates a new SDK client.
func NewSDKClient(endpoint, username, password string, timeout time.Duration, logger *slog.Logger) (*SDKClient, error) {
	// 创建 gojenkins 实例
	jenkins := gojenkins.CreateJenkins(nil, endpoint, username, password)

	// 初始化连接（需要 context）
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	_, err := jenkins.Init(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Jenkins SDK: %w", err)
	}

	logger.Info("Jenkins SDK 客户端初始化成功",
		"endpoint", endpoint,
	)

	return &SDKClient{
		jenkins: jenkins,
		logger:  logger,
	}, nil
}

// excludedFolders 是需要排除的文件夹列表（不采集这些文件夹下的 job）
var excludedFolders = map[string]bool{
	"prod-ebpay-new":  true,
	"pre-ebpay-new":   true,
	"prod-gray-ebpay":  true,
}

// JobWithPath wraps a gojenkins.Job with its full path.
// This is needed because gojenkins.Job.GetName() may return relative names for nested jobs.
type JobWithPath struct {
	Job     *gojenkins.Job
	FullPath string
}

// GetAllJobsRecursive recursively gets all jobs from specified folders, filtering out folder-type jobs.
// Returns jobs and a map of job to full path (e.g., "folder/job").
// The path map is needed because gojenkins.Job.GetName() may return relative names for nested jobs.
func (c *SDKClient) GetAllJobsRecursive(ctx context.Context, folderNames []string, logger *slog.Logger) ([]*gojenkins.Job, map[*gojenkins.Job]string, error) {
	allJobs := make([]*gojenkins.Job, 0)
	jobPathMap := make(map[*gojenkins.Job]string)

	// 如果没有指定文件夹，获取根目录下的所有内容
	if len(folderNames) == 0 {
		// 获取根目录下的所有 job（包括文件夹）
		// 注意：gojenkins.GetAllJobs() 可能只返回顶层 job，不递归
		// 所以我们需要手动递归处理每个 job
		rootJobs, err := c.jenkins.GetAllJobs(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get root jobs: %w", err)
		}

		logger.Debug("获取到根目录下的顶层 job",
			"顶层 job 数量", len(rootJobs),
		)

		// 递归处理每个 job（可能是文件夹或实际 job）
		for i, job := range rootJobs {
			// 检查 context 是否已取消
			if ctx.Err() != nil {
				return allJobs, jobPathMap, ctx.Err()
			}

			jobName := job.GetName()
			
			// 检查是否是排除的文件夹
			if excludedFolders[jobName] {
				logger.Debug("跳过排除的文件夹",
					"folder_name", jobName,
				)
				continue
			}

			logger.Debug("处理顶层 job",
				"序号", i+1,
				"总数", len(rootJobs),
				"job_name", jobName,
			)

			// 记录顶层 job 的路径
			jobPathMap[job] = jobName
			
			jobs, paths, err := c.recursiveGetJobsWithPathMap(ctx, job, jobName, jobPathMap, logger)
			if err != nil {
				// 如果是 context canceled，直接返回
				if errors.Is(err, context.Canceled) || ctx.Err() == context.Canceled {
					return allJobs, jobPathMap, err
				}
				logger.Warn("递归获取 job 失败",
					"job_name", jobName,
					"error", err,
				)
				continue
			}
			allJobs = append(allJobs, jobs...)
			// 合并路径映射
			for k, v := range paths {
				jobPathMap[k] = v
			}
		}
	} else {
		// 如果指定了文件夹，只处理这些文件夹
		for _, folderName := range folderNames {
			// 获取文件夹
			folderJob, err := c.jenkins.GetJob(ctx, folderName)
			if err != nil {
				logger.Warn("获取文件夹失败",
					"folder_name", folderName,
					"error", err,
				)
				continue
			}

			// 记录文件夹的路径
			jobPathMap[folderJob] = folderName
			
			// 递归获取文件夹下的所有 job
			jobs, paths, err := c.recursiveGetJobsWithPathMap(ctx, folderJob, folderName, jobPathMap, logger)
			if err != nil {
				logger.Warn("递归获取文件夹下的 job 失败",
					"folder_name", folderName,
					"error", err,
				)
				continue
			}
			allJobs = append(allJobs, jobs...)
			// 合并路径映射
			for k, v := range paths {
				jobPathMap[k] = v
			}
		}
	}

	logger.Info("递归获取 job 列表完成",
		"总数", len(allJobs),
		"指定文件夹", folderNames,
	)

	return allJobs, jobPathMap, nil
}

// recursiveGetJobsWithPathMap recursively gets all jobs and tracks their full paths.
// This ensures we always use the full path (folder/job) instead of just job name.
func (c *SDKClient) recursiveGetJobsWithPathMap(ctx context.Context, job *gojenkins.Job, fullPath string, jobPathMap map[*gojenkins.Job]string, logger *slog.Logger) ([]*gojenkins.Job, map[*gojenkins.Job]string, error) {
	allJobs := make([]*gojenkins.Job, 0)

	jobName := fullPath // 使用传入的完整路径
	// 记录当前 job 的完整路径
	jobPathMap[job] = fullPath
	
	// 检查是否是排除的文件夹（检查完整路径中的任何部分）
	// 例如：如果 jobName 是 "prod-gray-ebpay/some-job"，需要检查路径的第一部分
	parts := strings.Split(jobName, "/")
	if len(parts) > 0 {
		topLevelFolder := parts[0]
		if excludedFolders[topLevelFolder] {
			logger.Debug("跳过排除的文件夹路径",
				"job_name", jobName,
				"顶层文件夹", topLevelFolder,
			)
			return allJobs, jobPathMap, nil // 返回空列表，不递归处理
		}
	}

	// 检查是否是文件夹类型
	isFolder := false
	if job.Raw != nil {
		jobClass := job.Raw.Class
		if jobClass != "" && strings.Contains(jobClass, "Folder") {
			isFolder = true
		}
	}

	if isFolder {
		// 如果是文件夹，获取文件夹下的所有内容
		// gojenkins 使用 GetInnerJobs(ctx) 获取文件夹下的子项
		// 注意：即使 job.Raw.Jobs 是 nil，也应该尝试调用 GetInnerJobs
		// 因为 SDK 可能会在调用时自动获取子项
		subJobs, err := job.GetInnerJobs(ctx)
		if err != nil {
			// 如果获取失败，可能不是文件夹或没有权限
			logger.Debug("获取文件夹下的子项失败",
				"folder_name", job.GetName(),
				"error", err,
			)
			return allJobs, jobPathMap, nil // 返回空列表，不中断
		}

		logger.Debug("文件夹下的子项",
			"folder_name", fullPath,
			"子项数量", len(subJobs),
		)

		// 递归处理每个子项
		parentName := fullPath // 使用完整路径作为父路径
		for _, subJob := range subJobs {
			// 检查 context 是否已取消
			if ctx.Err() != nil {
				return allJobs, jobPathMap, ctx.Err()
			}

			subJobName := subJob.GetName()
			// 构建完整路径：如果子 job 的名称不包含父路径，则拼接
			// gojenkins SDK 的 GetName() 可能返回相对名称，需要拼接父路径
			var fullSubJobName string
			if strings.Contains(subJobName, "/") {
				// 如果已经包含路径分隔符，说明是完整路径
				fullSubJobName = subJobName
			} else {
				// 如果不包含路径分隔符，需要拼接父路径
				fullSubJobName = parentName + "/" + subJobName
			}

			logger.Debug("处理子 job",
				"父路径", parentName,
				"子 job 名称", subJobName,
				"完整路径", fullSubJobName,
			)

			// 递归处理子 job，传递完整路径
			jobs, paths, err := c.recursiveGetJobsWithPathMap(ctx, subJob, fullSubJobName, jobPathMap, logger)
			if err != nil {
				// 如果是 context canceled，直接返回
				if errors.Is(err, context.Canceled) || ctx.Err() == context.Canceled {
					return allJobs, jobPathMap, err
				}
				logger.Debug("递归获取子 job 失败",
					"parent", parentName,
					"child", subJobName,
					"full_path", fullSubJobName,
					"error", err,
				)
				continue
			}
			allJobs = append(allJobs, jobs...)
			// 合并路径映射
			for k, v := range paths {
				jobPathMap[k] = v
			}
		}
	} else {
		// 如果不是文件夹，就是实际的构建 job，直接添加
		// 注意：job 对象本身可能只包含相对名称，但我们使用 fullPath 作为完整路径
		allJobs = append(allJobs, job)
		jobPathMap[job] = fullPath
	}

	return allJobs, jobPathMap, nil
}

// recursiveGetJobs recursively gets all jobs from a job (which might be a folder).
// This is a wrapper that uses the job's GetName() and handles path construction.
func (c *SDKClient) recursiveGetJobs(ctx context.Context, job *gojenkins.Job, logger *slog.Logger) ([]*gojenkins.Job, error) {
	jobName := job.GetName()
	jobPathMap := make(map[*gojenkins.Job]string)
	jobs, _, err := c.recursiveGetJobsWithPathMap(ctx, job, jobName, jobPathMap, logger)
	return jobs, err
}

// GetAllJobs returns all jobs recursively, optionally filtered by folder names.
// It filters out folder-type jobs and only returns actual build jobs.
// Deprecated: Use GetAllJobsRecursive instead.
func (c *SDKClient) GetAllJobs(ctx context.Context, folderNames []string) ([]*gojenkins.Job, error) {
	allJobs := make([]*gojenkins.Job, 0)

	// 获取所有 job（递归）
	jobs, err := c.jenkins.GetAllJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get all jobs: %w", err)
	}

	c.logger.Debug("SDK 返回的原始 job 列表",
		"总数", len(jobs),
	)

	// 过滤掉文件夹类型的 job，只保留实际的构建 job
	filteredJobs := make([]*gojenkins.Job, 0)
	folderCount := 0
	
	for _, job := range jobs {
		// 检查 job 是否是文件夹类型
		isFolder := false
		
		// 方法1: 检查 Raw.Class 字段
		if job.Raw != nil {
			jobClass := job.Raw.Class
			if jobClass != "" {
				// 如果是文件夹类型，跳过
				if strings.Contains(jobClass, "Folder") {
					isFolder = true
					folderCount++
					c.logger.Debug("跳过文件夹类型的 job",
						"job_name", job.GetName(),
						"class", jobClass,
					)
				}
			}
		}
		
		// 方法2: 尝试获取构建信息，如果失败可能是文件夹
		if !isFolder {
			// 尝试获取最后一次构建，如果失败且是特定错误，可能是文件夹
			_, err := job.GetLastCompletedBuild(ctx)
			if err != nil {
				errMsg := err.Error()
				// 如果是 404 或找不到构建，可能是文件夹
				if strings.Contains(errMsg, "404") || 
				   strings.Contains(errMsg, "not found") ||
				   strings.Contains(errMsg, "invalid character '<'") {
					// 进一步检查：如果 job 没有构建历史，可能是文件夹
					// 但有些 job 确实没有构建，所以不能完全依赖这个
					// 主要依赖 class 字段判断
				}
			}
		}
		
		// 如果不是文件夹，添加到结果列表
		if !isFolder {
			filteredJobs = append(filteredJobs, job)
		}
	}

	c.logger.Debug("过滤后的 job 列表",
		"原始总数", len(jobs),
		"文件夹数量", folderCount,
		"实际 job 数量", len(filteredJobs),
	)

	// 如果指定了文件夹，进行过滤
	if len(folderNames) > 0 {
		folderSet := make(map[string]bool)
		for _, name := range folderNames {
			folderSet[name] = true
		}

		for _, job := range filteredJobs {
			// 检查 job 是否在指定的文件夹下
			fullName := job.GetName()
			parts := strings.Split(fullName, "/")
			if len(parts) > 0 {
				topFolder := parts[0]
				if folderSet[topFolder] {
					allJobs = append(allJobs, job)
				}
			}
		}
	} else {
		allJobs = filteredJobs
	}

	c.logger.Info("获取到 job 列表",
		"原始总数", len(jobs),
		"文件夹数量", folderCount,
		"过滤后总数", len(filteredJobs),
		"最终返回数量", len(allJobs),
		"指定文件夹", folderNames,
	)

	return allJobs, nil
}

// GetJobByFullName gets a job by its full name (e.g., "folder/job").
func (c *SDKClient) GetJobByFullName(ctx context.Context, fullName string) (*gojenkins.Job, error) {
	// gojenkins 的 GetJob 方法支持完整路径
	// fullName 应该是完整路径，例如 "folder/job" 或 "folder/subfolder/job"
	// 如果是顶层 job，fullName 就是 job 名称本身，例如 "job"
	c.logger.Debug("使用完整路径获取 job",
		"full_name", fullName,
		"说明", "如果 job 在文件夹下，路径格式为 folder/job；如果是顶层 job，就是 job 名称本身",
	)
	
	job, err := c.jenkins.GetJob(ctx, fullName)
	if err != nil {
		// 检查错误信息，判断是否是 HTML 响应（可能是认证失败、404、权限问题等）
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid character '<'") || strings.Contains(errMsg, "looking for beginning of value") {
			// 可能是文件夹而不是实际的 job，或者权限问题，或者路径不正确
			c.logger.Debug("获取 job 失败，可能是文件夹、权限问题或路径不正确",
				"job_name", fullName,
				"错误", errMsg,
				"提示", "如果 job 在文件夹下，确保使用完整路径格式 folder/job",
			)
			return nil, fmt.Errorf("job %s 可能不存在、是文件夹或权限不足（返回了 HTML 而非 JSON）: %w", fullName, err)
		}
		c.logger.Debug("获取 job 失败",
			"job_name", fullName,
			"错误", errMsg,
		)
		return nil, fmt.Errorf("failed to get job %s: %w", fullName, err)
	}

	// 检查 job 是否是文件夹（Folder 类型）
	// gojenkins 中，文件夹也有 GetName() 方法，但可能没有构建
	// 注意：Raw 字段可能不存在，需要安全访问
	if job.Raw != nil {
		jobClass := job.Raw.Class
		if jobClass != "" {
			c.logger.Debug("job 类型",
				"job_name", fullName,
				"class", jobClass,
			)
			// 如果是文件夹类型，可能需要特殊处理
			if strings.Contains(jobClass, "Folder") {
				c.logger.Debug("检测到文件夹类型，跳过",
					"job_name", fullName,
				)
				return nil, fmt.Errorf("job %s 是文件夹类型，不是实际的构建 job", fullName)
			}
		}
	}

	return job, nil
}

// GetLastCompletedBuild gets the last completed build for a job.
func (c *SDKClient) GetLastCompletedBuild(ctx context.Context, fullName string) (*gojenkins.Build, int64, error) {
	// 检查 context 是否已取消
	if ctx.Err() != nil {
		return nil, 0, ctx.Err()
	}

	job, err := c.GetJobByFullName(ctx, fullName)
	if err != nil {
		// 如果是 context canceled，直接返回
		if errors.Is(err, context.Canceled) || ctx.Err() == context.Canceled || strings.Contains(err.Error(), "context canceled") {
			return nil, 0, context.Canceled
		}
		return nil, 0, err
	}

	// 再次检查 context（在调用 SDK 前）
	if ctx.Err() != nil {
		return nil, 0, ctx.Err()
	}

	// 获取最后一次完成的构建
	build, err := job.GetLastCompletedBuild(ctx)
	if err != nil {
		// 如果是 context canceled，直接返回
		if errors.Is(err, context.Canceled) || ctx.Err() == context.Canceled || strings.Contains(err.Error(), "context canceled") {
			return nil, 0, context.Canceled
		}
		// 如果没有完成的构建，返回 nil
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("failed to get last completed build for job %s: %w", fullName, err)
	}

	buildNumber := int64(build.GetBuildNumber())

	return build, buildNumber, nil
}

// GetBuildDetails gets build details including parameters.
func (c *SDKClient) GetBuildDetails(ctx context.Context, build *gojenkins.Build) (*BuildDetails, error) {
	details := &BuildDetails{
		Number:     int64(build.GetBuildNumber()),
		Result:     build.GetResult(),
		Building:   build.IsRunning(ctx),
		Parameters: make(map[string]string),
	}

	// 获取时间戳（GetTimestamp 返回 time.Time，不是指针）
	timestamp := build.GetTimestamp()
	if !timestamp.IsZero() {
		details.Timestamp = timestamp.Unix()
	}

	// 获取持续时间（GetDuration 返回 float64，转换为 int64）
	duration := build.GetDuration()
	details.Duration = int64(duration)

	// 获取构建参数（GetParameters 不需要 context，只返回一个值）
	params := build.GetParameters()
	if params != nil {
		for _, param := range params {
			if param.Name != "" {
				// 将值转换为字符串
				var valueStr string
				switch v := param.Value.(type) {
				case string:
					valueStr = v
				case nil:
					valueStr = ""
				default:
					valueStr = fmt.Sprintf("%v", v)
				}
				details.Parameters[param.Name] = valueStr
			}
		}
	}

	return details, nil
}

// BuildDetails contains build information.
type BuildDetails struct {
	Number     int64
	Result     string
	Building   bool
	Timestamp  int64
	Duration   int64
	Parameters map[string]string
}

