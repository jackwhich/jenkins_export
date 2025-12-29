package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bndr/gojenkins"
)

// 示例1: 获取所有 job（包括文件夹）
func example1_GetAllJobs() {
	jenkins := gojenkins.CreateJenkins(nil, "http://jenkins.example.com", "username", "password")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := jenkins.Init(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// 获取所有顶层 job
	allJobs, err := jenkins.GetAllJobs(ctx)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("找到 %d 个 job\n", len(allJobs))
	for _, job := range allJobs {
		fmt.Printf("- %s\n", job.GetName())
	}
}

// 示例2: 递归获取文件夹下的所有 job
func example2_GetJobsFromFolder() {
	jenkins := gojenkins.CreateJenkins(nil, "http://jenkins.example.com", "username", "password")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := jenkins.Init(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// 获取文件夹
	folderName := "prod-gray-ebpay"
	folderJob, err := jenkins.GetJob(ctx, folderName)
	if err != nil {
		log.Fatal(err)
	}

	// 递归获取文件夹下的所有 job
	allJobs := getAllJobsRecursive(ctx, folderJob)
	fmt.Printf("文件夹 %s 下共有 %d 个 job:\n", folderName, len(allJobs))
	for _, job := range allJobs {
		fmt.Printf("- %s\n", job.GetName())
	}
}

// 示例3: 获取指定 job 的详细信息
func example3_GetJobDetails() {
	jenkins := gojenkins.CreateJenkins(nil, "http://jenkins.example.com", "username", "password")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := jenkins.Init(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// 获取指定的 job
	jobName := "prod-gray-ebpay/gray-prod-mkt-thirdpart-api"
	job, err := jenkins.GetJob(ctx, jobName)
	if err != nil {
		log.Fatal(err)
	}

	// 打印 job 信息
	fmt.Printf("Job 名称: %s\n", job.GetName())
	if job.Raw != nil {
		fmt.Printf("Job 类型: %s\n", job.Raw.Class)
		fmt.Printf("Job URL: %s\n", job.Raw.URL)
	}

	// 获取最后一次构建
	lastBuild, err := job.GetLastCompletedBuild(ctx)
	if err != nil {
		fmt.Printf("无构建记录\n")
	} else {
		fmt.Printf("最后构建: #%d (%s)\n", lastBuild.GetBuildNumber(), lastBuild.GetResult())
		fmt.Printf("构建时间: %v\n", lastBuild.GetTimestamp())
		fmt.Printf("构建时长: %d ms\n", lastBuild.GetDuration())

		// 获取构建参数
		params := lastBuild.GetParameters()
		if len(params) > 0 {
			fmt.Println("构建参数:")
			for _, param := range params {
				fmt.Printf("  %s = %v\n", param.Name, param.Value)
			}
		}
	}
}

// 示例4: 获取多个文件夹下的所有 job
func example4_GetJobsFromMultipleFolders() {
	jenkins := gojenkins.CreateJenkins(nil, "http://jenkins.example.com", "username", "password")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := jenkins.Init(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// 要获取的文件夹列表
	folders := []string{"prod-gray-ebpay", "prod-marketing", "uat"}

	allJobs := make([]*gojenkins.Job, 0)
	for _, folderName := range folders {
		folderJob, err := jenkins.GetJob(ctx, folderName)
		if err != nil {
			fmt.Printf("获取文件夹 %s 失败: %v\n", folderName, err)
			continue
		}

		// 递归获取文件夹下的所有 job
		jobs := getAllJobsRecursive(ctx, folderJob)
		allJobs = append(allJobs, jobs...)
		fmt.Printf("文件夹 %s: 找到 %d 个 job\n", folderName, len(jobs))
	}

	fmt.Printf("\n总共找到 %d 个 job\n", len(allJobs))
}

// 辅助函数: 递归获取文件夹下的所有 job
func getAllJobsRecursive(ctx context.Context, job *gojenkins.Job) []*gojenkins.Job {
	allJobs := make([]*gojenkins.Job, 0)

	// 检查是否是文件夹
	if isFolder(job) {
		// 获取文件夹下的子项
		if job.Raw != nil && job.Raw.Jobs != nil {
			subJobs, err := job.GetInnerJobs(ctx)
			if err != nil {
				return allJobs
			}

			// 递归处理每个子项
			for _, subJob := range subJobs {
				jobs := getAllJobsRecursive(ctx, subJob)
				allJobs = append(allJobs, jobs...)
			}
		}
	} else {
		// 实际的构建 job，直接添加
		allJobs = append(allJobs, job)
	}

	return allJobs
}

// 辅助函数: 检查是否是文件夹
func isFolder(job *gojenkins.Job) bool {
	if job.Raw != nil {
		jobClass := job.Raw.Class
		if jobClass != "" && strings.Contains(jobClass, "Folder") {
			return true
		}
	}
	return false
}

func main() {
	fmt.Println("=== 示例1: 获取所有 job ===")
	// example1_GetAllJobs()

	fmt.Println("\n=== 示例2: 递归获取文件夹下的所有 job ===")
	// example2_GetJobsFromFolder()

	fmt.Println("\n=== 示例3: 获取指定 job 的详细信息 ===")
	// example3_GetJobDetails()

	fmt.Println("\n=== 示例4: 获取多个文件夹下的所有 job ===")
	// example4_GetJobsFromMultipleFolders()

	fmt.Println("\n请取消注释上面的函数调用来运行示例")
}

