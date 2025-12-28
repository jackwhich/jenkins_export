// Package exporter provides Prometheus collectors for Jenkins metrics.
package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/promhippie/jenkins_exporter/pkg/config"
	"github.com/promhippie/jenkins_exporter/pkg/internal/jenkins"
)

// JobCollector collects metrics about the servers.
type JobCollector struct {
	client            *jenkins.Client
	logger            *slog.Logger
	failures          *prometheus.CounterVec
	duration          *prometheus.HistogramVec
	config            config.Target
	fetchBuildDetails bool
	cacheFile         string
	cacheTTL          time.Duration
	folders           []string // 要获取的文件夹列表，如果为空则获取所有文件夹
	cacheMutex        sync.RWMutex
	lastCacheUpdate   time.Time

	Disabled        *prometheus.Desc
	Duration        *prometheus.Desc
	StartTime       *prometheus.Desc
	EndTime         *prometheus.Desc
	BuildLastResult *prometheus.Desc
}

// NewJobCollector returns a new JobCollector.
func NewJobCollector(logger *slog.Logger, client *jenkins.Client, failures *prometheus.CounterVec, duration *prometheus.HistogramVec, cfg config.Target, fetchBuildDetails bool, cacheFile string, cacheTTL time.Duration, folders []string) *JobCollector {
	if failures != nil {
		failures.WithLabelValues("job").Add(0)
	}

	labels := []string{"job_name"} // job_name 就是 job 的完整路径，不需要 name 和 class
	return &JobCollector{
		client:            client,
		logger:            logger.With("collector", "job"),
		failures:          failures,
		duration:          duration,
		config:            cfg,
		fetchBuildDetails: fetchBuildDetails,
		cacheFile:         cacheFile,
		cacheTTL:          cacheTTL,
		folders:            folders,

		Disabled: prometheus.NewDesc(
			"jenkins_job_disabled",
			"1 if the job is disabled, 0 otherwise",
			labels,
			nil,
		),
		Duration: prometheus.NewDesc(
			"jenkins_job_duration",
			"Duration of last build in ms",
			labels,
			nil,
		),
		StartTime: prometheus.NewDesc(
			"jenkins_job_start_time",
			"Start time of last build as unix timestamp",
			labels,
			nil,
		),
		EndTime: prometheus.NewDesc(
			"jenkins_job_end_time",
			"Start time of last build as unix timestamp",
			labels,
			nil,
		),
		BuildLastResult: prometheus.NewDesc(
			"jenkins_build_last_result",
			"Last build result: 1 indicates current status, status label contains the actual status (success, failure, aborted, waiting, in_progress, not_built)",
			[]string{"job_name", "check_commitID", "gitBranch", "status"}, // 只包含4个标签：job_name, check_commitID, gitBranch, status
			nil,
		),
	}
}

// InitializeCache initializes the cache file at startup.
// If cache file doesn't exist or is expired, it will fetch jobs from Jenkins API and save to cache.
func (c *JobCollector) InitializeCache(ctx context.Context) error {
	if c.cacheFile == "" {
		return nil // 未启用缓存
	}

	// 检查缓存文件是否存在且有效
	if cachedJobs, fromCache, needsUpdate := c.loadJobsFromCache(); fromCache && !needsUpdate {
		c.logger.Info("缓存文件已存在且有效，无需初始化",
			"缓存文件", c.cacheFile,
			"作业数量", len(cachedJobs),
		)
		return nil
	}

	// 缓存文件不存在或已过期，需要初始化
	c.logger.Info("正在初始化缓存文件",
		"缓存文件", c.cacheFile,
		"说明", "缓存文件不存在或已过期，将从 Jenkins API 获取作业列表并保存到缓存",
	)

	// 从 API 获取作业列表
	if len(c.folders) > 0 {
		c.logger.Info("开始从 Jenkins API 获取指定文件夹的作业",
			"文件夹", c.folders,
			"说明", fmt.Sprintf("将递归遍历指定文件夹 %v 获取所有作业", c.folders),
		)
	} else {
		c.logger.Info("开始从 Jenkins API 获取所有作业",
			"说明", "将递归遍历所有文件夹（如 uat, pro, prod-gray-ebpay 等）获取所有作业",
		)
	}

	jobs, err := c.client.Job.All(ctx, c.folders)
	if err != nil {
		return fmt.Errorf("初始化缓存失败，无法从 Jenkins 获取作业列表: %w", err)
	}

	// 保存到缓存
	if err := c.saveJobsToCache(jobs); err != nil {
		return fmt.Errorf("初始化缓存失败，无法保存到缓存文件: %w", err)
	}

	c.logger.Info("缓存文件初始化完成",
		"缓存文件", c.cacheFile,
		"作业数量", len(jobs),
		"说明", fmt.Sprintf("已成功获取 %d 个作业，包括所有文件夹（如 uat, pro 等）下的所有作业", len(jobs)),
	)

	return nil
}

// Metrics simply returns the list metric descriptors for generating a documentation.
func (c *JobCollector) Metrics() []*prometheus.Desc {
	return []*prometheus.Desc{
		c.Disabled,
		c.Duration,
		c.StartTime,
		c.EndTime,
		c.BuildLastResult,
	}
}

// Describe sends the super-set of all possible descriptors of metrics collected by this Collector.
func (c *JobCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.Disabled
	ch <- c.Duration
	ch <- c.StartTime
	ch <- c.EndTime
	ch <- c.BuildLastResult
}

// loadJobsFromCache loads jobs from cache file if it exists.
// Returns (jobs, fromCache, needsUpdate)
// fromCache: true if loaded from cache, false if cache doesn't exist
// needsUpdate: true if cache is expired and needs background update
func (c *JobCollector) loadJobsFromCache() ([]jenkins.Job, bool, bool) {
	if c.cacheFile == "" {
		return nil, false, false
	}

	c.cacheMutex.RLock()
	defer c.cacheMutex.RUnlock()

	// 检查缓存文件是否存在
	info, err := os.Stat(c.cacheFile)
	if err != nil {
		// 首次运行，缓存文件不存在是正常的
		c.logger.Debug("缓存文件不存在，将从 API 获取（首次运行或缓存文件被删除）",
			"缓存文件", c.cacheFile,
		)
		return nil, false, false
	}

	// 读取缓存文件
	data, err := os.ReadFile(c.cacheFile)
	if err != nil {
		c.logger.Warn("读取缓存文件失败，将从 API 获取",
			"缓存文件", c.cacheFile,
			"错误", err,
		)
		return nil, false, false
	}

	var jobs []jenkins.Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		c.logger.Warn("解析缓存文件失败，将从 API 获取",
			"缓存文件", c.cacheFile,
			"错误", err,
		)
		return nil, false, false
	}

	// 检查缓存是否过期
	age := time.Since(info.ModTime())
	needsUpdate := age > c.cacheTTL

	if needsUpdate {
		c.logger.Info("缓存已过期，将使用缓存数据并后台更新",
			"缓存文件", c.cacheFile,
			"修改时间", info.ModTime(),
			"缓存年龄", age,
			"过期时间", c.cacheTTL,
			"作业数量", len(jobs),
		)
	} else {
		c.logger.Info("从缓存文件加载作业列表",
			"缓存文件", c.cacheFile,
			"作业数量", len(jobs),
			"缓存时间", info.ModTime(),
		)
	}

	return jobs, true, needsUpdate
}

// saveJobsToCache saves jobs to cache file.
func (c *JobCollector) saveJobsToCache(jobs []jenkins.Job) error {
	if c.cacheFile == "" {
		return nil
	}

	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	// 确保目录存在
	dir := filepath.Dir(c.cacheFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建缓存目录失败: %w", err)
	}

	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化作业数据失败: %w", err)
	}

	if err := os.WriteFile(c.cacheFile, data, 0644); err != nil {
		return fmt.Errorf("写入缓存文件失败: %w", err)
	}

	c.lastCacheUpdate = time.Now()
	c.logger.Info("已保存作业列表到缓存文件",
		"缓存文件", c.cacheFile,
		"作业数量", len(jobs),
	)

	return nil
}

// updateCacheInBackground updates cache in background without blocking.
func (c *JobCollector) updateCacheInBackground() {
	c.logger.Info("开始后台更新缓存",
		"缓存文件", c.cacheFile,
	)

	ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
	defer cancel()

	jobs, err := c.client.Job.All(ctx, c.folders)
	if err != nil {
		c.logger.Warn("后台更新缓存失败",
			"错误", err,
		)
		return
	}

	if err := c.saveJobsToCache(jobs); err != nil {
		c.logger.Warn("后台保存缓存失败",
			"错误", err,
		)
		return
	}

	c.logger.Info("后台更新缓存完成",
		"作业数量", len(jobs),
	)
}

// Collect is called by the Prometheus registry when collecting metrics.
func (c *JobCollector) Collect(ch chan<- prometheus.Metric) {
	c.logger.Info("开始收集作业指标",
		"超时时间", c.config.Timeout,
		"获取构建详情", c.fetchBuildDetails,
		"缓存文件", c.cacheFile,
		"缓存TTL", c.cacheTTL,
	)

	// 先尝试从缓存加载
	var jobs []jenkins.Job
	var elapsed time.Duration
	var ctx context.Context
	var cancel context.CancelFunc

	if cachedJobs, fromCache, needsUpdate := c.loadJobsFromCache(); fromCache {
		jobs = cachedJobs
		elapsed = 0 // 从缓存加载，耗时几乎为0
		c.logger.Info("使用缓存数据",
			"作业数量", len(jobs),
			"需要后台更新", needsUpdate,
		)

		// 如果缓存过期，后台异步更新（不阻塞当前请求）
		if needsUpdate {
			go c.updateCacheInBackground()
		}
	} else {
		// 从 API 获取
		ctx, cancel = context.WithTimeout(context.Background(), c.config.Timeout)
		defer cancel()

		now := time.Now()
		c.logger.Info("正在从 Jenkins 获取作业列表")

		c.logger.Info("获取作业流程说明",
			"步骤1", "调用 Jenkins API 获取根目录: /api/json?depth=1",
			"步骤2", "递归遍历所有文件夹（包括所有层级的文件夹）",
			"步骤3", "对每个文件夹调用: /job/{folder}/api/json?depth=1",
			"步骤4", "获取文件夹内的所有作业: /job/{folder}/job/{job}/api/json",
		)

		c.logger.Info("步骤1: 正在获取 Jenkins 根目录信息",
			"说明", "通过 /api/json?depth=1 获取根目录下的所有文件夹和作业",
		)

		var err error
		jobs, err = c.client.Job.All(ctx, c.folders)
		elapsed = time.Since(now)
		c.duration.WithLabelValues("job").Observe(elapsed.Seconds())

		if err != nil {
			c.logger.Error("获取作业列表失败",
				"错误", err,
				"耗时秒", elapsed.Seconds(),
				"说明", "可能是网络超时或Jenkins服务器无响应，请检查网络连接和Jenkins地址",
			)

			c.failures.WithLabelValues("job").Inc()
			return
		}

		// 保存到缓存
		if err := c.saveJobsToCache(jobs); err != nil {
			c.logger.Warn("保存缓存失败",
				"错误", err,
			)
		}
	}

	// 统计各个文件夹下的作业数量
	folderJobCount := make(map[string]int)
	for _, job := range jobs {
		// job.Path 格式可能是 "uat/job-name" 或 "prod-gray-ebpay/job-name"
		parts := strings.Split(job.Path, "/")
		if len(parts) > 0 {
			folder := parts[0]
			folderJobCount[folder]++
		}
	}

	c.logger.Info("成功获取作业列表",
		"作业数量", len(jobs),
		"耗时秒", elapsed.Seconds(),
		"说明", fmt.Sprintf("已递归遍历所有文件夹（/job/ 路径下），成功获取到 %d 个作业", len(jobs)),
		"各文件夹作业数", folderJobCount,
	)

	c.logger.Info("开始处理作业并导出指标",
		"总作业数", len(jobs),
		"获取构建详情", c.fetchBuildDetails,
	)

	processedCount := 0
	buildDetailsFetched := 0
	buildDetailsFailed := 0

	// 如果启用构建详情获取，使用并行处理
	if c.fetchBuildDetails {
		// 并行获取构建详情
		type buildDetailResult struct {
			job           jenkins.Job
			build         jenkins.Build
			buildErr      error
			checkCommitID string
			gitBranch     string
			status        float64
		}

		// 创建 worker pool，最多10个并发
		const maxWorkers = 10
		jobsChan := make(chan jenkins.Job, len(jobs))
		resultsChan := make(chan buildDetailResult, len(jobs))

		// 启动 workers
		var wg sync.WaitGroup
		for w := 0; w < maxWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range jobsChan {
					if job.LastBuild == nil {
						resultsChan <- buildDetailResult{job: job}
						continue
					}

					buildCtx, buildCancel := context.WithTimeout(context.Background(), 5*time.Second)
					build, buildErr := c.client.Job.Build(buildCtx, job.LastBuild)
					buildCancel()

					result := buildDetailResult{
						job:      job,
						build:    build,
						buildErr: buildErr,
					}

					if buildErr == nil {
						result.checkCommitID = extractParameter(build, "check_commitID")
						result.gitBranch = extractParameter(build, "gitBranch")
						result.status = buildStatusToValue(build.Result, build.Building, build.QueueID)
					}

					resultsChan <- result
				}
			}()
		}

		// 发送所有作业到 channel
		go func() {
			for _, job := range jobs {
				jobsChan <- job
			}
			close(jobsChan)
		}()

		// 等待所有 workers 完成
		go func() {
			wg.Wait()
			close(resultsChan)
		}()

		// 收集结果并处理
		buildDetailsMap := make(map[string]buildDetailResult)
		for result := range resultsChan {
			if result.buildErr == nil {
				buildDetailsFetched++
				buildDetailsMap[result.job.Path] = result
			} else {
				buildDetailsFailed++
				buildDetailsMap[result.job.Path] = result
			}
		}

		// 处理所有作业
		for i, job := range jobs {
			// 每处理10个作业记录一次进度
			if i > 0 && i%10 == 0 {
				c.logger.Info("正在处理作业",
					"进度", fmt.Sprintf("%d/%d", i, len(jobs)),
					"当前作业", job.Path,
					"已处理", processedCount,
				)
			}
			var (
				disabled float64
			)

			labels := []string{
				job.Path, // path 就是 jobname，不需要 name 和 class
			}

			if job.Disabled {
				disabled = 1.0
			}

			ch <- prometheus.MustNewConstMetric(
				c.Disabled,
				prometheus.GaugeValue,
				disabled,
				labels...,
			)

			if job.LastBuild != nil {
				// 从并行获取的结果中获取构建详情
				var checkCommitID, gitBranch string
				var status float64
				result, hasResult := buildDetailsMap[job.Path]

				if hasResult && result.buildErr == nil {
					// 成功获取构建详情
					checkCommitID = result.checkCommitID
					gitBranch = result.gitBranch
					status = result.status

					// 导出构建详情指标
					ch <- prometheus.MustNewConstMetric(
						c.Duration,
						prometheus.GaugeValue,
						float64(result.build.Duration),
						labels...,
					)

					ch <- prometheus.MustNewConstMetric(
						c.StartTime,
						prometheus.GaugeValue,
						float64(result.build.Timestamp),
						labels...,
					)

					ch <- prometheus.MustNewConstMetric(
						c.EndTime,
						prometheus.GaugeValue,
						float64(result.build.Timestamp+result.build.Duration),
						labels...,
					)
				} else {
					// 获取失败或未获取，使用作业颜色推断状态
					switch job.Color {
					case "blue", "blue_anime":
						status = 0.0 // success
					case "red", "red_anime":
						status = 1.0 // failure
					case "aborted", "aborted_anime":
						status = 2.0 // aborted
					case "yellow", "yellow_anime":
						status = 3.0 // unstable
					default:
						status = 6.0 // not_built
					}
					checkCommitID = "" // 无法获取
					gitBranch = ""     // 无法获取
				}

				// 根据状态值确定 status 标签
				var statusLabel string
				if status == 0.0 {
					statusLabel = "success"
				} else if status == 1.0 {
					statusLabel = "failure"
				} else if status == 2.0 {
					statusLabel = "aborted"
				} else if status == 4.0 {
					statusLabel = "in_progress"
				} else if status == 5.0 {
					statusLabel = "waiting"
				} else {
					statusLabel = "not_built"
				}

				// 导出统一的构建结果指标，值为1表示当前状态，通过status标签区分
				// 只包含4个标签：job_name, check_commitID, gitBranch, status
				labelsBuildResult := []string{
					job.Path,      // job_name
					checkCommitID, // check_commitID
					gitBranch,     // gitBranch
					statusLabel,   // status
				}
				ch <- prometheus.MustNewConstMetric(
					c.BuildLastResult,
					prometheus.GaugeValue,
					1.0, // 值为1表示这是当前状态
					labelsBuildResult...,
				)
			} else {
				// 如果没有 LastBuild，仍然导出构建结果指标（未构建状态）
				// 只包含4个标签：job_name, check_commitID, gitBranch, status
				labelsBuildResult := []string{
					job.Path,    // job_name
					"",          // check_commitID
					"",          // gitBranch
					"not_built", // status
				}
				ch <- prometheus.MustNewConstMetric(
					c.BuildLastResult,
					prometheus.GaugeValue,
					1.0, // 值为1表示这是当前状态
					labelsBuildResult...,
				)
			}

			processedCount++
		}
	} else {
		// 未启用构建详情获取，串行处理
		for i, job := range jobs {
			// 每处理10个作业记录一次进度
			if i > 0 && i%10 == 0 {
				c.logger.Info("正在处理作业",
					"进度", fmt.Sprintf("%d/%d", i, len(jobs)),
					"当前作业", job.Path,
					"已处理", processedCount,
				)
			}
			var (
				disabled float64
			)

			labels := []string{
				job.Path, // path 就是 jobname，不需要 name 和 class
			}

			if job.Disabled {
				disabled = 1.0
			}

			ch <- prometheus.MustNewConstMetric(
				c.Disabled,
				prometheus.GaugeValue,
				disabled,
				labels...,
			)

			if job.LastBuild != nil {
				// 未启用构建详情，使用作业颜色推断状态
				var statusLabel string
				switch job.Color {
				case "blue", "blue_anime":
					statusLabel = "success"
				case "red", "red_anime":
					statusLabel = "failure"
				case "aborted", "aborted_anime":
					statusLabel = "aborted"
				case "yellow", "yellow_anime":
					statusLabel = "unstable"
				default:
					statusLabel = "not_built"
				}

				// 导出统一的构建结果指标
				// 只包含4个标签：job_name, check_commitID, gitBranch, status
				labelsBuildResult := []string{
					job.Path,
					"", // check_commitID
					"", // gitBranch
					statusLabel,
				}

				ch <- prometheus.MustNewConstMetric(
					c.BuildLastResult,
					prometheus.GaugeValue,
					1.0,
					labelsBuildResult...,
				)
			} else {
				// 如果没有 LastBuild，仍然导出构建结果指标（未构建状态）
				// 只包含4个标签：job_name, check_commitID, gitBranch, status
				labelsBuildResult := []string{
					job.Path,
					"",          // check_commitID
					"",          // gitBranch
					"not_built", // status
				}

				ch <- prometheus.MustNewConstMetric(
					c.BuildLastResult,
					prometheus.GaugeValue,
					1.0,
					labelsBuildResult...,
				)
			}

			processedCount++
		}
	}

	c.logger.Info("作业指标收集完成",
		"总作业数", len(jobs),
		"已处理作业数", processedCount,
		"成功获取构建详情数", buildDetailsFetched,
		"获取构建详情失败数", buildDetailsFailed,
		"构建详情获取已启用", c.fetchBuildDetails,
	)
}

// extractParameter extracts a parameter value from build actions.
func extractParameter(build jenkins.Build, paramName string) string {
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

// buildStatusToValue converts build status to numeric value.
// 0=success, 1=failure, 2=aborted, 3=unstable, 4=in_progress, 5=queued, 6=not_built
func buildStatusToValue(result string, building bool, queueID int64) float64 {
	// 如果正在构建，返回 in_progress
	if building {
		return 4.0 // 正在构建
	}

	// 优先检查构建结果，如果 result 有值，说明构建已完成，不应该判断为等待
	// queueID 可能是历史记录，不应该影响已完成构建的状态
	switch result {
	case "SUCCESS":
		return 0.0
	case "FAILURE":
		return 1.0
	case "ABORTED":
		return 2.0
	case "UNSTABLE":
		return 3.0
	}

	// 如果 result 为空且 queueID > 0，可能是等待中（但这种情况很少，因为通常没有构建记录）
	// 实际上，对于已完成的构建，queueID 可能仍然有值，但 result 应该有值
	// 如果 result 为空且 building == false，更可能是未构建状态
	if queueID > 0 && result == "" {
		return 5.0 // 等待中（理论上可能，但实际很少见）
	}

	return 6.0 // 未构建
}
