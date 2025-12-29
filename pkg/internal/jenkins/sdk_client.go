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

// GetAllJobs returns all jobs recursively, optionally filtered by folder names.
func (c *SDKClient) GetAllJobs(ctx context.Context, folderNames []string) ([]*gojenkins.Job, error) {
	allJobs := make([]*gojenkins.Job, 0)

	// 获取所有 job（递归）
	jobs, err := c.jenkins.GetAllJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get all jobs: %w", err)
	}

	// 如果指定了文件夹，进行过滤
	if len(folderNames) > 0 {
		folderSet := make(map[string]bool)
		for _, name := range folderNames {
			folderSet[name] = true
		}

		for _, job := range jobs {
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
		allJobs = jobs
	}

	c.logger.Debug("获取到 job 列表",
		"总数", len(allJobs),
		"指定文件夹", folderNames,
	)

	return allJobs, nil
}

// GetJobByFullName gets a job by its full name (e.g., "folder/job").
func (c *SDKClient) GetJobByFullName(ctx context.Context, fullName string) (*gojenkins.Job, error) {
	// gojenkins 的 GetJob 方法支持完整路径
	job, err := c.jenkins.GetJob(ctx, fullName)
	if err != nil {
		return nil, fmt.Errorf("failed to get job %s: %w", fullName, err)
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

