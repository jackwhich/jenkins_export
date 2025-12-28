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

// All returns all available jobs.
func (c *JobClient) All(ctx context.Context) ([]Job, error) {
	hudson, err := c.Root(ctx)

	if err != nil {
		return []Job{}, err
	}

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
					// 成功获取文件夹内容，检查是否有子文件夹或作业
					if len(nextFolder.Folders) > 0 {
						// 有子文件夹或作业，递归处理所有内容
						// 注意：Folders 字段映射自 JSON 的 "jobs" 字段，可能包含文件夹和作业
						jobs, err = c.recursiveFoldersParallel(ctx, nextFolder.Folders, maxConcurrency)
						if err != nil {
							errMu.Lock()
							if firstErr == nil {
								firstErr = err
							}
							errMu.Unlock()
						}
					}
					
					// 即使有子文件夹，当前 URL 本身也可能是一个作业（某些 Jenkins 插件支持）
					// 尝试获取当前 URL 作为作业，如果成功则添加到结果中
					req, reqErr := c.client.NewRequest(ctx, "GET", fmt.Sprintf("%s/api/json", url), nil)
					if reqErr == nil {
						job := Job{}
						if _, reqErr := c.client.Do(req, &job); reqErr == nil {
							// 检查是否已经通过递归获取到了这个作业（避免重复）
							// 通过比较 Path 来判断
							isDuplicate := false
							for _, existingJob := range jobs {
								if existingJob.Path == job.Path {
									isDuplicate = true
									break
								}
							}
							if !isDuplicate {
								jobs = append(jobs, job)
							}
						}
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
