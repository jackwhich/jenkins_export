package jenkins

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// JobClient is a client for the jobs API.
type JobClient struct {
	client *Client
}

// Root returns a root API response.
func (c *JobClient) Root(ctx context.Context) (Hudson, error) {
	result := Hudson{}
	req, err := c.client.NewRequest(ctx, "GET", fmt.Sprintf("%s/api/json?depth=1", c.client.endpoint), nil)

	if err != nil {
		return result, err
	}

	if _, err := c.client.Do(req, &result); err != nil {
		return result, err
	}

	return result, nil
}

// Build returns a specific build.
func (c *JobClient) Build(ctx context.Context, build *BuildNumber) (Build, error) {
	result := Build{}
	url := strings.TrimRight(build.URL, "/")
	req, err := c.client.NewRequest(ctx, "GET", fmt.Sprintf("%s/api/json", url), nil)

	if err != nil {
		return result, err
	}

	if _, err := c.client.Do(req, &result); err != nil {
		return result, err
	}

	return result, nil
}

// GetLastCompletedBuild returns the last completed build for a job by job name (full path).
// Returns (build, buildNumber, nil) if found, or (nil, 0, nil) if no completed build exists.
func (c *JobClient) GetLastCompletedBuild(ctx context.Context, jobName string) (*Build, int64, error) {
	// 构建 job API URL
	// jobName 格式可能是 "folder/job" 或 "folder/subfolder/job"
	// 需要转换为 Jenkins API 路径格式：/job/folder/job/folder/job/...
	pathParts := strings.Split(jobName, "/")
	apiPath := ""
	for _, part := range pathParts {
		if part != "" {
			apiPath += "/job/" + part
		}
	}

	// 获取 job 信息
	jobURL := fmt.Sprintf("%s%s/api/json", c.client.endpoint, apiPath)
	req, err := c.client.NewRequest(ctx, "GET", jobURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request for job %s: %w", jobName, err)
	}

	var job Job
	if _, err := c.client.Do(req, &job); err != nil {
		return nil, 0, fmt.Errorf("failed to get job %s: %w", jobName, err)
	}

	// 如果没有 lastCompletedBuild，返回 nil
	if job.LastCompletedBuild == nil {
		return nil, 0, nil
	}

	buildNumber := int64(job.LastCompletedBuild.Number)

	// 获取构建详情
	build, err := c.Build(ctx, job.LastCompletedBuild)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get build details for job %s: %w", jobName, err)
	}

	return &build, buildNumber, nil
}

// All returns all available jobs.
// If folders is not empty, only jobs from the specified folders will be returned.
func (c *JobClient) All(ctx context.Context, folders []string) ([]Job, error) {
	hudson, err := c.Root(ctx)

	if err != nil {
		return []Job{}, err
	}

	// 如果指定了文件夹，只处理这些文件夹
	if len(folders) > 0 {
		// 创建文件夹名称到文件夹的映射
		folderMap := make(map[string]Folder)
		allTopLevelFolders := make([]string, 0)
		for _, folder := range hudson.Folders {
			folderMap[folder.Name] = folder
			allTopLevelFolders = append(allTopLevelFolders, folder.Name)
		}

		// 只处理指定的文件夹
		filteredFolders := make([]Folder, 0)
		notFoundFolders := make([]string, 0)
		for _, folderName := range folders {
			if folder, exists := folderMap[folderName]; exists {
				filteredFolders = append(filteredFolders, folder)
			} else {
				notFoundFolders = append(notFoundFolders, folderName)
			}
		}

		// 如果有些文件夹不存在，在错误信息中包含详细信息
		if len(notFoundFolders) > 0 {
			// 仍然继续处理找到的文件夹，但错误信息会包含警告
			if len(filteredFolders) == 0 {
				return []Job{}, fmt.Errorf("指定的文件夹不存在: %v (可用的顶层文件夹: %v)", folders, allTopLevelFolders)
			}
			// 如果有部分文件夹不存在，仍然处理找到的文件夹，但返回错误信息
			// 注意：这里不返回错误，而是继续处理，让调用者决定如何处理
		}

		if len(filteredFolders) == 0 {
			return []Job{}, fmt.Errorf("指定的文件夹不存在: %v (可用的顶层文件夹: %v)", folders, allTopLevelFolders)
		}

		jobs, err := c.recursiveFolders(ctx, filteredFolders)
		if err != nil {
			return []Job{}, err
		}
		return jobs, nil
	}

	// 如果没有指定文件夹，获取所有文件夹下的作业
	jobs, err := c.recursiveFolders(ctx, hudson.Folders)

	if err != nil {
		return []Job{}, err
	}

	return jobs, nil
}

func (c *JobClient) recursiveFolders(ctx context.Context, folders []Folder) ([]Job, error) {
	return c.recursiveFoldersParallel(ctx, folders, 10) // 最多10个并发
}

func (c *JobClient) recursiveFoldersParallel(ctx context.Context, folders []Folder, maxConcurrency int) ([]Job, error) {
	if len(folders) == 0 {
		return []Job{}, nil
	}

	// 使用 channel 限制并发数
	semaphore := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	result := make([]Job, 0)
	
	// 用于收集错误，但不中断处理
	var firstErr error
	var errMu sync.Mutex

	for _, folder := range folders {
		// 检查上下文是否已取消
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		wg.Add(1)
		go func(f Folder) {
			defer wg.Done()
			
			// 获取信号量
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			var jobs []Job
			var err error

			// 先尝试获取文件夹内容，检查是否有子文件夹或作业
			// 这样可以处理所有类型的文件夹，不仅仅是 com.cloudbees.hudson.plugins.folder.Folder
			// 注意：depth=1 只获取直接子项，不会递归获取所有层级
			url := strings.TrimRight(f.URL, "/")
			req, reqErr := c.client.NewRequest(ctx, "GET", fmt.Sprintf("%s/api/json?depth=1", url), nil)

			if reqErr != nil {
				// 如果请求失败，尝试作为作业处理
				req, reqErr = c.client.NewRequest(ctx, "GET", fmt.Sprintf("%s/api/json", url), nil)
				if reqErr != nil {
					return // 跳过
				}

				job := Job{}
				if _, reqErr := c.client.Do(req, &job); reqErr != nil {
					return // 跳过
				}

				jobs = []Job{job}
			} else {
				// 尝试作为文件夹处理
				nextFolder := Folder{}
				if _, reqErr := c.client.Do(req, &nextFolder); reqErr != nil {
					// 如果解析失败，尝试作为作业处理
					req, reqErr = c.client.NewRequest(ctx, "GET", fmt.Sprintf("%s/api/json", url), nil)
					if reqErr != nil {
						return // 跳过
					}

					job := Job{}
					if _, reqErr := c.client.Do(req, &job); reqErr != nil {
						return // 跳过
					}

					jobs = []Job{job}
				} else {
					// 检查 _class 字段判断是文件夹还是作业
					// 如果是文件夹类型，递归处理其内容
					// 如果是作业类型，直接获取作业
					isFolder := nextFolder.Class == "com.cloudbees.hudson.plugins.folder.Folder" || 
					           strings.Contains(nextFolder.Class, "Folder")
					
					if isFolder {
						// 这是文件夹，递归处理其内容
						// 注意：Folders 字段映射自 JSON 的 "jobs" 字段，包含该文件夹下的所有内容（文件夹和作业）
						// 即使文件夹为空，也要继续处理，因为可能有作业在下一层
						if len(nextFolder.Folders) > 0 {
							// 有子文件夹或作业，递归处理所有内容
							jobs, err = c.recursiveFoldersParallel(ctx, nextFolder.Folders, maxConcurrency)
							if err != nil {
								errMu.Lock()
								if firstErr == nil {
									firstErr = err
								}
								errMu.Unlock()
							}
						}
						// 注意：如果文件夹为空（len(nextFolder.Folders) == 0），不处理任何内容
						// 这是正常的，因为空文件夹下没有作业
					} else {
						// 这是作业，直接获取作业详情
						// 即使 _class 不是明确的作业类型，只要不是文件夹，就当作作业处理
						req, reqErr := c.client.NewRequest(ctx, "GET", fmt.Sprintf("%s/api/json", url), nil)
						if reqErr != nil {
							return // 跳过
						}

						job := Job{}
						if _, reqErr := c.client.Do(req, &job); reqErr != nil {
							return // 跳过
						}

						jobs = []Job{job}
					}
				}
			}

			// 线程安全地追加结果
			if len(jobs) > 0 {
				mu.Lock()
				result = append(result, jobs...)
				mu.Unlock()
			}
		}(folder)
	}

	wg.Wait()
	return result, firstErr
}
