package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/bndr/gojenkins"
)

func main() {
	// 1. 创建 Jenkins 客户端
	// ⚠️ 请修改为你的 Jenkins 连接信息
	jenkinsURL := "http://jenkins.example.com"  // 改为你的 Jenkins URL
	username := "your_username"                  // 改为你的用户名
	password := "your_password"                  // 改为你的密码
	
	// 或者从环境变量读取
	if jenkinsURL == "http://jenkins.example.com" {
		if url := os.Getenv("JENKINS_URL"); url != "" {
			jenkinsURL = url
		}
	}
	if username == "your_username" {
		if user := os.Getenv("JENKINS_USERNAME"); user != "" {
			username = user
		}
	}
	if password == "your_password" {
		if pass := os.Getenv("JENKINS_PASSWORD"); pass != "" {
			password = pass
		}
	}

	jenkins := gojenkins.CreateJenkins(nil, jenkinsURL, username, password)

	// 2. 初始化连接
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := jenkins.Init(ctx)
	if err != nil {
		log.Fatalf("连接 Jenkins 失败: %v", err)
	}
	fmt.Println("✅ 成功连接到 Jenkins")

	// 3. 获取所有顶层 job
	fmt.Println("\n=== 方法1: 获取所有顶层 job ===")
	allJobs, err := jenkins.GetAllJobs(ctx)
	if err != nil {
		log.Fatalf("获取所有 job 失败: %v", err)
	}
	fmt.Printf("找到 %d 个顶层 job\n", len(allJobs))

	// 4. 检查每个 job 是文件夹还是实际 job
	for _, job := range allJobs {
		jobName := job.GetName()
		if isFolder(job) {
			fmt.Printf("📁 文件夹: %s\n", jobName)
		} else {
			fmt.Printf("✅ Job: %s\n", jobName)
		}
	}

	// 5. 获取指定文件夹下的所有 job（递归）
	fmt.Println("\n=== 方法2: 递归获取指定文件夹下的所有 job ===")
	folderName := "prod-gray-ebpay"
	fmt.Printf("正在获取文件夹: %s\n", folderName)
	
	folderJob, err := jenkins.GetJob(ctx, folderName)
	if err != nil {
		fmt.Printf("⚠️  获取文件夹失败: %v\n", err)
		fmt.Println("跳过方法2，继续执行其他方法...")
	} else {
		fmt.Printf("✅ 成功获取文件夹: %s\n", folderName)
		
		// 显示文件夹信息
		if folderJob.Raw != nil {
			fmt.Printf("文件夹类型: %s\n", folderJob.Raw.Class)
		}
		
		// 递归获取文件夹下的所有 job
		fmt.Println("开始递归获取文件夹下的所有 job...")
		allJobsInFolder := getAllJobsRecursive(ctx, folderJob, 0)
		fmt.Printf("\n文件夹 %s 下共有 %d 个 job:\n", folderName, len(allJobsInFolder))
		if len(allJobsInFolder) > 0 {
			for i, job := range allJobsInFolder {
				fmt.Printf("%d. %s\n", i+1, job.GetName())
			}
		} else {
			fmt.Println("  (文件夹下没有找到实际的构建 job)")
		}
	}

	// 6. 获取指定 job 的详细信息
	fmt.Println("\n=== 方法3: 获取指定 job 的详细信息 ===")
	specificJobName := "prod-gray-ebpay/gray-prod-mkt-thirdpart-api"
	fmt.Printf("正在获取 job: %s\n", specificJobName)
	
	job, err := jenkins.GetJob(ctx, specificJobName)
	if err != nil {
		fmt.Printf("⚠️  获取 job 失败: %v\n", err)
		fmt.Println("跳过方法3，继续执行其他方法...")
	} else {
		fmt.Printf("✅ 成功获取 job: %s\n", specificJobName)
		printJobDetails(job, ctx)

		// 7. 获取 job 的最后一次构建
		fmt.Println("\n=== 方法4: 获取 job 的最后一次构建 ===")
		lastBuild, err := job.GetLastCompletedBuild(ctx)
		if err != nil {
			fmt.Printf("⚠️  获取最后构建失败: %v\n", err)
		} else {
			fmt.Printf("✅ 成功获取最后构建\n")
			fmt.Printf("最后构建编号: #%d\n", lastBuild.GetBuildNumber())
			fmt.Printf("构建结果: %s\n", lastBuild.GetResult())
			fmt.Printf("构建时间: %v\n", lastBuild.GetTimestamp())
			fmt.Printf("构建时长: %d ms\n", lastBuild.GetDuration())

			// 获取构建参数
			params := lastBuild.GetParameters()
			if len(params) > 0 {
				fmt.Println("构建参数:")
				for _, param := range params {
					fmt.Printf("  - %s: %v\n", param.Name, param.Value)
				}
			} else {
				fmt.Println("构建参数: 无")
			}
		}
	}
	
	fmt.Println("\n=== 所有方法执行完成 ===")
}

// isFolder 检查是否是文件夹
func isFolder(job *gojenkins.Job) bool {
	if job.Raw != nil {
		jobClass := job.Raw.Class
		if jobClass != "" && strings.Contains(jobClass, "Folder") {
			return true
		}
	}
	return false
}

// getAllJobsRecursive 递归获取文件夹下的所有 job
func getAllJobsRecursive(ctx context.Context, job *gojenkins.Job, depth int) []*gojenkins.Job {
	allJobs := make([]*gojenkins.Job, 0)
	indent := strings.Repeat("  ", depth)

	// 检查是否是文件夹
	if isFolder(job) {
		fmt.Printf("%s📁 处理文件夹: %s\n", indent, job.GetName())
		
		// 如果是文件夹，获取文件夹下的所有子项
		if job.Raw != nil && job.Raw.Jobs != nil {
			fmt.Printf("%s  正在获取子项...\n", indent)
			subJobs, err := job.GetInnerJobs(ctx)
			if err != nil {
				fmt.Printf("%s  ⚠️  获取子项失败: %v\n", indent, err)
				return allJobs
			}

			fmt.Printf("%s  找到 %d 个子项\n", indent, len(subJobs))
			
			// 递归处理每个子项
			for i, subJob := range subJobs {
				fmt.Printf("%s  处理子项 %d/%d: %s\n", indent, i+1, len(subJobs), subJob.GetName())
				jobs := getAllJobsRecursive(ctx, subJob, depth+1)
				allJobs = append(allJobs, jobs...)
			}
		} else {
			fmt.Printf("%s  (文件夹为空或无法获取子项)\n", indent)
		}
	} else {
		// 如果不是文件夹，就是实际的构建 job，直接添加
		fmt.Printf("%s✅ 找到 job: %s\n", indent, job.GetName())
		allJobs = append(allJobs, job)
	}

	return allJobs
}

// printJobDetails 打印 job 的详细信息
func printJobDetails(job *gojenkins.Job, ctx context.Context) {
	fmt.Printf("Job 名称: %s\n", job.GetName())

	if job.Raw != nil {
		fmt.Printf("Job 类型: %s\n", job.Raw.Class)
		if job.Raw.URL != "" {
			fmt.Printf("Job URL: %s\n", job.Raw.URL)
		}
		if job.Raw.Description != "" {
			fmt.Printf("Job 描述: %s\n", job.Raw.Description)
		}
	}

	// 获取 job 的构建信息
	lastBuild, err := job.GetLastCompletedBuild(ctx)
	if err == nil && lastBuild != nil {
		fmt.Printf("最后构建: #%d (%s)\n", lastBuild.GetBuildNumber(), lastBuild.GetResult())
	} else {
		fmt.Printf("最后构建: 无\n")
	}

	// 获取 job 的配置信息（如果可用）
	if job.Raw != nil {
		if job.Raw.Color != "" {
			fmt.Printf("Job 状态: %s\n", job.Raw.Color)
		}
		if job.Raw.HealthReport != nil && len(job.Raw.HealthReport) > 0 {
			fmt.Println("健康报告:")
			for _, report := range job.Raw.HealthReport {
				fmt.Printf("  - %s: %s\n", report.Description, report.Score)
			}
		}
	}
}

