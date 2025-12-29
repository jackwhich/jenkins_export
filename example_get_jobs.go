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
	jenkinsURL := "http://jenkins.example.com" // 改为你的 Jenkins URL
	username := "your_username"                 // 改为你的用户名
	password := "your_password"                // 改为你的密码

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

	// 2. 初始化连接（增加超时时间到 5 分钟，因为递归获取可能需要较长时间）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_, err := jenkins.Init(ctx)
	if err != nil {
		log.Fatalf("连接 Jenkins 失败: %v", err)
	}
	fmt.Println("✅ 成功连接到 Jenkins")
	fmt.Println("📝 说明: 使用 gojenkins SDK，SDK 内部通过 REST API 实现")
	fmt.Println("   错误信息中显示的 API 调用是 SDK 内部的正常行为\n")

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
	fmt.Println("使用 SDK 方法: jenkins.GetJob() -> job.GetInnerJobs()")
	folderName := "prod-gray-ebpay"
	fmt.Printf("正在获取文件夹: %s\n", folderName)

	// 声明变量在外部作用域，以便后续方法使用
	var allJobsInFolder []*gojenkins.Job

	folderJob, err := jenkins.GetJob(ctx, folderName) // SDK 方法
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
		fmt.Println("提示: 如果 job 很多，可能需要较长时间，请耐心等待...")
		allJobsInFolder = getAllJobsRecursive(ctx, folderJob, 0)
		
		// 检查是否超时
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Printf("\n⚠️  操作超时！已获取到 %d 个 job（可能还有更多）\n", len(allJobsInFolder))
			fmt.Println("建议: 增加超时时间或分批处理")
		}
		
		fmt.Printf("\n文件夹 %s 下共有 %d 个 job:\n", folderName, len(allJobsInFolder))
		if len(allJobsInFolder) > 0 {
			// 显示所有 job（不限制数量）
			for i, job := range allJobsInFolder {
				fmt.Printf("%d. %s\n", i+1, job.GetName())
			}
		} else {
			fmt.Println("  (文件夹下没有找到实际的构建 job 或获取超时)")
		}
	}

	// 6. 获取指定 job 的详细信息
	fmt.Println("\n=== 方法3: 获取指定 job 的详细信息 ===")
	fmt.Println("使用 SDK 方法: jenkins.GetJob()")
	
	// 从方法2获取的job列表中取一个来测试
	if len(allJobsInFolder) > 0 {
		// 使用第一个job来测试
		testJob := allJobsInFolder[0]
		specificJobName := testJob.GetName()
		fmt.Printf("正在获取 job: %s\n", specificJobName)
		fmt.Printf("提示: 使用从方法2获取到的job名称，确保路径正确\n")

		// 方法1: 直接使用从GetInnerJobs获取的job对象（推荐）
		fmt.Println("\n方法3a: 直接使用已获取的job对象（推荐）")
		printJobDetails(testJob, ctx)

		// 方法2: 通过job名称重新获取（测试）
		fmt.Println("\n方法3b: 通过job名称重新获取（测试）")
		job, err := jenkins.GetJob(ctx, specificJobName) // SDK 方法
		if err != nil {
			fmt.Printf("⚠️  通过名称重新获取失败: %v\n", err)
			fmt.Printf("   建议: 直接使用已获取的job对象，避免重复获取\n")
		} else {
			fmt.Printf("✅ 成功通过名称获取 job: %s\n", specificJobName)
			printJobDetails(job, ctx)
		}
	} else {
		fmt.Println("⚠️  方法2没有获取到job，跳过方法3")
	}
	
	// 原来的测试代码（如果方法2失败时使用）
	if len(allJobsInFolder) == 0 {
		specificJobName := "prod-gray-ebpay/gray-prod-mkt-thirdpart-api"
		fmt.Printf("\n尝试获取指定 job: %s\n", specificJobName)
		job, err := jenkins.GetJob(ctx, specificJobName) // SDK 方法
		if err != nil {
			fmt.Printf("⚠️  获取 job 失败: %v\n", err)
			fmt.Printf("   可能原因:\n")
			fmt.Printf("   1. job 路径格式不正确\n")
			fmt.Printf("   2. job 不存在或权限不足\n")
			fmt.Printf("   3. SDK 在处理嵌套路径时有问题\n")
			fmt.Printf("   建议: 使用方法2获取job列表，然后直接使用job对象\n")
		} else {
			fmt.Printf("✅ 成功获取 job: %s\n", specificJobName)
			printJobDetails(job, ctx)
		}
	}

	// 7. 获取 job 的最后一次构建（使用已获取的job对象）
	if len(allJobsInFolder) > 0 {
		fmt.Println("\n=== 方法4: 获取 job 的最后一次构建 ===")
		testJob := allJobsInFolder[0]
		fmt.Printf("使用 job: %s\n", testJob.GetName())
		
		lastBuild, err := testJob.GetLastCompletedBuild(ctx)
		if err != nil {
			fmt.Printf("⚠️  获取最后构建失败: %v\n", err)
			if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
				fmt.Printf("   说明: 该 job 还没有已完成的构建\n")
			}
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

	// 检查 context 是否已超时
	if ctx.Err() != nil {
		fmt.Printf("%s⚠️  操作超时，停止处理\n", indent)
		return allJobs
	}

	// 检查是否是文件夹
	if isFolder(job) {
		fmt.Printf("%s📁 处理文件夹: %s\n", indent, job.GetName())

		// 如果是文件夹，获取文件夹下的所有子项
		if job.Raw != nil && job.Raw.Jobs != nil {
			fmt.Printf("%s  正在获取子项（使用 SDK: job.GetInnerJobs()）...\n", indent)

			// 为每个操作创建子 context，避免单个操作超时影响整体
			// 注意: GetInnerJobs() 是 SDK 方法，内部会调用 REST API
			subCtx, subCancel := context.WithTimeout(ctx, 60*time.Second) // 增加到 60 秒
			subJobs, err := job.GetInnerJobs(subCtx)                      // 这是 SDK 方法
			subCancel()

			if err != nil {
				// 检查是否是超时错误
				if ctx.Err() == context.DeadlineExceeded {
					fmt.Printf("%s  ⚠️  获取子项超时（可能是 job 太多，建议增加超时时间）: %v\n", indent, err)
				} else {
					fmt.Printf("%s  ⚠️  获取子项失败: %v\n", indent, err)
				}
				return allJobs
			}

			fmt.Printf("%s  找到 %d 个子项\n", indent, len(subJobs))

			// 递归处理每个子项（不限制深度，获取所有 job）
			for i, subJob := range subJobs {
				// 检查 context 是否已超时
				if ctx.Err() != nil {
					fmt.Printf("%s  ⚠️  操作超时，已处理 %d/%d 个子项\n", indent, i, len(subJobs))
					break
				}

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
