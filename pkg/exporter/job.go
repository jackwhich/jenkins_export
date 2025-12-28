// Package exporter provides Prometheus collectors for Jenkins metrics.
package exporter

import (
	"context"
	"fmt"
	"log/slog"
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

	Disabled              *prometheus.Desc
	Buildable             *prometheus.Desc
	Color                 *prometheus.Desc
	LastBuild             *prometheus.Desc
	LastCompletedBuild    *prometheus.Desc
	LastFailedBuild       *prometheus.Desc
	LastStableBuild       *prometheus.Desc
	LastSuccessfulBuild   *prometheus.Desc
	LastUnstableBuild     *prometheus.Desc
	LastUnsuccessfulBuild *prometheus.Desc
	NextBuild             *prometheus.Desc
	Duration              *prometheus.Desc
	StartTime             *prometheus.Desc
	EndTime               *prometheus.Desc
	BuildStatus           *prometheus.Desc
}

// NewJobCollector returns a new JobCollector.
func NewJobCollector(logger *slog.Logger, client *jenkins.Client, failures *prometheus.CounterVec, duration *prometheus.HistogramVec, cfg config.Target, fetchBuildDetails bool) *JobCollector {
	if failures != nil {
		failures.WithLabelValues("job").Add(0)
	}

	labels := []string{"name", "path", "class"}
	labelsWithParams := []string{"name", "path", "class", "check_commitID", "gitBranch"}
	return &JobCollector{
		client:            client,
		logger:            logger.With("collector", "job"),
		failures:          failures,
		duration:          duration,
		config:            cfg,
		fetchBuildDetails: fetchBuildDetails,

		Disabled: prometheus.NewDesc(
			"jenkins_job_disabled",
			"1 if the job is disabled, 0 otherwise",
			labels,
			nil,
		),
		Buildable: prometheus.NewDesc(
			"jenkins_job_buildable",
			"1 if the job is buildable, 0 otherwise",
			labels,
			nil,
		),
		Color: prometheus.NewDesc(
			"jenkins_job_color",
			"Color code of the jenkins job",
			labels,
			nil,
		),
		LastBuild: prometheus.NewDesc(
			"jenkins_job_last_build",
			"Builder number for last build",
			labels,
			nil,
		),
		LastCompletedBuild: prometheus.NewDesc(
			"jenkins_job_last_completed_build",
			"Builder number for last completed build",
			labels,
			nil,
		),
		LastFailedBuild: prometheus.NewDesc(
			"jenkins_job_last_failed_build",
			"Builder number for last failed build",
			labels,
			nil,
		),
		LastStableBuild: prometheus.NewDesc(
			"jenkins_job_last_stable_build",
			"Builder number for last stable build",
			labels,
			nil,
		),
		LastSuccessfulBuild: prometheus.NewDesc(
			"jenkins_job_last_successful_build",
			"Builder number for last successful build",
			labels,
			nil,
		),
		LastUnstableBuild: prometheus.NewDesc(
			"jenkins_job_last_unstable_build",
			"Builder number for last unstable build",
			labels,
			nil,
		),
		LastUnsuccessfulBuild: prometheus.NewDesc(
			"jenkins_job_last_unsuccessful_build",
			"Builder number for last unsuccessful build",
			labels,
			nil,
		),
		NextBuild: prometheus.NewDesc(
			"jenkins_job_next_build_number",
			"Next build number for the job",
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
		BuildStatus: prometheus.NewDesc(
			"jenkins_job_build_status",
			"Build status: 0=success, 1=failure, 2=aborted, 3=unstable, 4=in_progress, 5=queued, 6=not_built",
			labelsWithParams,
			nil,
		),
	}
}

// Metrics simply returns the list metric descriptors for generating a documentation.
func (c *JobCollector) Metrics() []*prometheus.Desc {
	return []*prometheus.Desc{
		c.Disabled,
		c.Buildable,
		c.Color,
		c.LastBuild,
		c.LastCompletedBuild,
		c.LastFailedBuild,
		c.LastStableBuild,
		c.LastSuccessfulBuild,
		c.LastUnstableBuild,
		c.LastUnsuccessfulBuild,
		c.NextBuild,
		c.Duration,
		c.StartTime,
		c.EndTime,
		c.BuildStatus,
	}
}

// Describe sends the super-set of all possible descriptors of metrics collected by this Collector.
func (c *JobCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.Disabled
	ch <- c.Buildable
	ch <- c.Color
	ch <- c.LastBuild
	ch <- c.LastCompletedBuild
	ch <- c.LastFailedBuild
	ch <- c.LastStableBuild
	ch <- c.LastSuccessfulBuild
	ch <- c.LastUnstableBuild
	ch <- c.LastUnsuccessfulBuild
	ch <- c.NextBuild
	ch <- c.Duration
	ch <- c.StartTime
	ch <- c.EndTime
	ch <- c.BuildStatus
}

// Collect is called by the Prometheus registry when collecting metrics.
func (c *JobCollector) Collect(ch chan<- prometheus.Metric) {
	c.logger.Info("开始收集作业指标",
		"超时时间", c.config.Timeout,
		"获取构建详情", c.fetchBuildDetails,
	)

	ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
	defer cancel()

	now := time.Now()
	c.logger.Info("正在从 Jenkins 获取作业列表")

	c.logger.Info("获取作业流程说明",
		"步骤1", "调用 Jenkins API 获取根目录: /api/json?depth=1",
		"步骤2", "递归遍历所有文件夹（如 uat, pro 等）",
		"步骤3", "对每个文件夹调用: /job/{folder}/api/json?depth=1",
		"步骤4", "获取文件夹内的作业: /job/{folder}/job/{job}/api/json",
	)

	c.logger.Info("步骤1: 正在获取 Jenkins 根目录信息",
		"说明", "通过 /api/json?depth=1 获取根目录下的所有文件夹和作业",
	)

	jobs, err := c.client.Job.All(ctx)
	elapsed := time.Since(now)
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

	c.logger.Info("成功获取作业列表",
		"作业数量", len(jobs),
		"耗时秒", elapsed.Seconds(),
		"说明", fmt.Sprintf("已递归遍历所有文件夹（/job/ 路径下），成功获取到 %d 个作业", len(jobs)),
	)

	c.logger.Info("开始处理作业并导出指标",
		"总作业数", len(jobs),
		"获取构建详情", c.fetchBuildDetails,
	)

	processedCount := 0
	buildDetailsFetched := 0
	buildDetailsFailed := 0

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
			disabled  float64
			buildable float64
		)

		labels := []string{
			job.Name,
			job.Path,
			job.Class,
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

		if job.Buildable {
			buildable = 1.0
		}

		ch <- prometheus.MustNewConstMetric(
			c.Buildable,
			prometheus.GaugeValue,
			buildable,
			labels...,
		)

		ch <- prometheus.MustNewConstMetric(
			c.Color,
			prometheus.GaugeValue,
			colorToGauge(job.Color),
			labels...,
		)

		if job.LastBuild != nil {
			ch <- prometheus.MustNewConstMetric(
				c.LastBuild,
				prometheus.GaugeValue,
				float64(job.LastBuild.Number),
				labels...,
			)

			// 根据配置决定是否获取构建详情
			var build jenkins.Build
			var buildErr error
			var checkCommitID, gitBranch string
			var status float64

			if c.fetchBuildDetails {
				c.logger.Debug("正在获取构建详情",
					"作业", job.Path,
					"构建号", job.LastBuild.Number,
				)

				// 为获取构建详情创建更短的超时上下文（最多5秒）
				buildStart := time.Now()
				buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
				build, buildErr = c.client.Job.Build(buildCtx, job.LastBuild)
				buildCancel()
				buildElapsed := time.Since(buildStart)

				if buildErr == nil {
					buildDetailsFetched++
					c.logger.Debug("成功获取构建详情",
						"作业", job.Path,
						"构建号", job.LastBuild.Number,
						"耗时毫秒", buildElapsed.Milliseconds(),
					)

					// 成功获取构建详情，提取参数和状态
					checkCommitID = extractParameter(build, "check_commitID")
					gitBranch = extractParameter(build, "gitBranch")
					status = buildStatusToValue(build.Result, build.Building, build.QueueID)

					c.logger.Debug("已提取构建参数",
						"作业", job.Path,
						"check_commitID", checkCommitID,
						"gitBranch", gitBranch,
						"状态", status,
						"构建结果", build.Result,
						"正在构建", build.Building,
					)

					// 导出构建详情指标
					ch <- prometheus.MustNewConstMetric(
						c.Duration,
						prometheus.GaugeValue,
						float64(build.Duration),
						labels...,
					)

					ch <- prometheus.MustNewConstMetric(
						c.StartTime,
						prometheus.GaugeValue,
						float64(build.Timestamp),
						labels...,
					)

					ch <- prometheus.MustNewConstMetric(
						c.EndTime,
						prometheus.GaugeValue,
						float64(build.Timestamp+build.Duration),
						labels...,
					)
				} else {
					buildDetailsFailed++
					c.logger.Warn("获取构建详情失败，使用作业颜色推断状态",
						"作业", job.Path,
						"构建号", job.LastBuild.Number,
						"错误", buildErr,
						"耗时毫秒", buildElapsed.Milliseconds(),
					)
					c.failures.WithLabelValues("job").Inc()
				}
			} else {
				c.logger.Debug("跳过构建详情获取（已禁用）",
					"作业", job.Path,
				)
			}

			// 如果未获取构建详情或获取失败，使用作业颜色推断状态
			if !c.fetchBuildDetails || buildErr != nil {
				// 根据作业颜色推断状态
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

			// 导出构建状态指标（无论是否获取到构建详情）
			labelsWithParams := []string{
				job.Name,
				job.Path,
				job.Class,
				checkCommitID,
				gitBranch,
			}

			ch <- prometheus.MustNewConstMetric(
				c.BuildStatus,
				prometheus.GaugeValue,
				status,
				labelsWithParams...,
			)
		} else {
			// 如果没有 LastBuild，仍然导出构建状态（未构建状态）
			// 使用空参数值
			labelsWithParams := []string{
				job.Name,
				job.Path,
				job.Class,
				"", // check_commitID
				"", // gitBranch
			}

			ch <- prometheus.MustNewConstMetric(
				c.BuildStatus,
				prometheus.GaugeValue,
				6.0, // not_built
				labelsWithParams...,
			)
		}

		if job.LastCompletedBuild != nil {
			ch <- prometheus.MustNewConstMetric(
				c.LastCompletedBuild,
				prometheus.GaugeValue,
				float64(job.LastCompletedBuild.Number),
				labels...,
			)
		}

		if job.LastFailedBuild != nil {
			ch <- prometheus.MustNewConstMetric(
				c.LastFailedBuild,
				prometheus.GaugeValue,
				float64(job.LastFailedBuild.Number),
				labels...,
			)
		}

		if job.LastStableBuild != nil {
			ch <- prometheus.MustNewConstMetric(
				c.LastStableBuild,
				prometheus.GaugeValue,
				float64(job.LastStableBuild.Number),
				labels...,
			)
		}

		if job.LastSuccessfulBuild != nil {
			ch <- prometheus.MustNewConstMetric(
				c.LastSuccessfulBuild,
				prometheus.GaugeValue,
				float64(job.LastSuccessfulBuild.Number),
				labels...,
			)
		}

		if job.LastUnstableBuild != nil {
			ch <- prometheus.MustNewConstMetric(
				c.LastUnstableBuild,
				prometheus.GaugeValue,
				float64(job.LastUnstableBuild.Number),
				labels...,
			)
		}

		if job.LastUnsuccessfulBuild != nil {
			ch <- prometheus.MustNewConstMetric(
				c.LastUnsuccessfulBuild,
				prometheus.GaugeValue,
				float64(job.LastUnsuccessfulBuild.Number),
				labels...,
			)
		}

		ch <- prometheus.MustNewConstMetric(
			c.NextBuild,
			prometheus.GaugeValue,
			float64(job.NextBuildNumber),
			labels...,
		)

		processedCount++
	}

	c.logger.Info("作业指标收集完成",
		"总作业数", len(jobs),
		"已处理作业数", processedCount,
		"成功获取构建详情数", buildDetailsFetched,
		"获取构建详情失败数", buildDetailsFailed,
		"构建详情获取已启用", c.fetchBuildDetails,
	)
}

func colorToGauge(color string) float64 {
	switch color {
	case "blue":
		return 1.0
	case "blue_anime":
		return 1.5
	case "red":
		return 2.0
	case "red_anime":
		return 2.5
	case "yellow":
		return 3.0
	case "yellow_anime":
		return 3.5
	case "notbuilt":
		return 4.0
	case "notbuilt_anime":
		return 4.5
	case "disabled":
		return 5.0
	case "disabled_anime":
		return 5.5
	case "aborted":
		return 6.0
	case "aborted_anime":
		return 6.5
	case "grey":
		return 7.0
	case "grey_anime":
		return 7.5
	}

	return 0.0
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
	if building {
		return 4.0 // 正在构建
	}

	if queueID > 0 {
		return 5.0 // 等待中
	}

	switch result {
	case "SUCCESS":
		return 0.0
	case "FAILURE":
		return 1.0
	case "ABORTED":
		return 2.0
	case "UNSTABLE":
		return 3.0
	default:
		return 6.0 // 未构建
	}
}
