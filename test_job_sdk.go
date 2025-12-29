package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/bndr/gojenkins"
)

func main() {
	var (
		jenkinsURL = flag.String("url", "", "Jenkins URL (required)")
		username   = flag.String("user", "", "Jenkins username")
		password   = flag.String("pass", "", "Jenkins password")
		folderName = flag.String("folder", "", "Folder name to get jobs from (optional)")
		jobName    = flag.String("job", "", "Specific job name to get (optional)")
		recursive  = flag.Bool("recursive", true, "Recursively get all jobs from folders")
		timeout    = flag.Duration("timeout", 30*time.Second, "Request timeout")
	)
	flag.Parse()

	if *jenkinsURL == "" {
		fmt.Fprintf(os.Stderr, "Error: Jenkins URL is required\n")
		flag.Usage()
		os.Exit(1)
	}

	// 从环境变量获取认证信息（如果命令行未提供）
	if *username == "" {
		*username = os.Getenv("JENKINS_USERNAME")
	}
	if *password == "" {
		*password = os.Getenv("JENKINS_PASSWORD")
	}

	fmt.Printf("连接到 Jenkins: %s\n", *jenkinsURL)
	if *username != "" {
		fmt.Printf("用户名: %s\n", *username)
	}

	// 创建 Jenkins 客户端
	jenkins := gojenkins.CreateJenkins(nil, *jenkinsURL, *username, *password)

	// 初始化连接
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	_, err := jenkins.Init(ctx)
	if err != nil {
		log.Fatalf("连接 Jenkins 失败: %v\n", err)
	}
	fmt.Println("✅ 成功连接到 Jenkins")

	// 如果指定了具体的 job，直接获取
	if *jobName != "" {
		fmt.Printf("\n获取指定的 job: %s\n", *jobName)
		job, err := jenkins.GetJob(ctx, *jobName)
		if err != nil {
			log.Fatalf("获取 job 失败: %v\n", err)
		}
		printJobInfo(job, 0)
		return
	}

	// 如果指定了文件夹，获取文件夹下的 job
	if *folderName != "" {
		fmt.Printf("\n获取文件夹下的 job: %s (递归: %v)\n", *folderName, *recursive)
		folderJob, err := jenkins.GetJob(ctx, *folderName)
		if err != nil {
			log.Fatalf("获取文件夹失败: %v\n", err)
		}

		fmt.Printf("\n文件夹信息:\n")
		printJobInfo(folderJob, 0)

		if *recursive {
			fmt.Printf("\n递归获取文件夹下的所有 job:\n")
			jobs := getAllJobsRecursive(ctx, folderJob, 0)
			fmt.Printf("\n总共找到 %d 个 job:\n", len(jobs))
			for i, job := range jobs {
				fmt.Printf("%d. %s\n", i+1, job.GetName())
			}
		} else {
			// 只获取直接子项
			if folderJob.Raw != nil && folderJob.Raw.Jobs != nil {
				subJobs, err := folderJob.GetInnerJobs(ctx)
				if err != nil {
					log.Printf("获取子项失败: %v\n", err)
				} else {
					fmt.Printf("\n直接子项 (%d 个):\n", len(subJobs))
					for i, job := range subJobs {
						fmt.Printf("%d. %s\n", i+1, job.GetName())
					}
				}
			}
		}
		return
	}

	// 获取所有 job
	fmt.Println("\n获取所有 job...")
	allJobs, err := jenkins.GetAllJobs(ctx)
	if err != nil {
		log.Fatalf("获取所有 job 失败: %v\n", err)
	}

	fmt.Printf("找到 %d 个顶层 job\n", len(allJobs))

	// 统计文件夹和实际 job
	folderCount := 0
	jobCount := 0
	for _, job := range allJobs {
		if isFolder(job) {
			folderCount++
		} else {
			jobCount++
		}
	}

	fmt.Printf("  文件夹: %d 个\n", folderCount)
	fmt.Printf("  实际 job: %d 个\n", jobCount)

	if *recursive {
		fmt.Println("\n递归获取所有 job（包括文件夹下的）...")
		allJobsRecursive := make([]*gojenkins.Job, 0)
		for _, job := range allJobs {
			jobs := getAllJobsRecursive(ctx, job, 0)
			allJobsRecursive = append(allJobsRecursive, jobs...)
		}
		fmt.Printf("\n总共找到 %d 个 job（递归）:\n", len(allJobsRecursive))
		for i, job := range allJobsRecursive {
			fmt.Printf("%d. %s\n", i+1, job.GetName())
		}
	} else {
		fmt.Println("\n顶层 job 列表:")
		for i, job := range allJobs {
			jobType := "job"
			if isFolder(job) {
				jobType = "folder"
			}
			fmt.Printf("%d. [%s] %s\n", i+1, jobType, job.GetName())
		}
	}
}

// getAllJobsRecursive 递归获取所有 job
func getAllJobsRecursive(ctx context.Context, job *gojenkins.Job, depth int) []*gojenkins.Job {
	allJobs := make([]*gojenkins.Job, 0)
	indent := strings.Repeat("  ", depth)

	if isFolder(job) {
		fmt.Printf("%s📁 文件夹: %s\n", indent, job.GetName())
		if job.Raw != nil && job.Raw.Jobs != nil {
			subJobs, err := job.GetInnerJobs(ctx)
			if err != nil {
				fmt.Printf("%s  ⚠️  获取子项失败: %v\n", indent, err)
				return allJobs
			}
			for _, subJob := range subJobs {
				jobs := getAllJobsRecursive(ctx, subJob, depth+1)
				allJobs = append(allJobs, jobs...)
			}
		}
	} else {
		fmt.Printf("%s✅ Job: %s\n", indent, job.GetName())
		allJobs = append(allJobs, job)
	}

	return allJobs
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

// printJobInfo 打印 job 信息
func printJobInfo(job *gojenkins.Job, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Printf("%s名称: %s\n", indent, job.GetName())
	if job.Raw != nil {
		fmt.Printf("%s类型: %s\n", indent, job.Raw.Class)
		if job.Raw.URL != "" {
			fmt.Printf("%sURL: %s\n", indent, job.Raw.URL)
		}
	}

	// 尝试获取构建信息
	ctx := context.Background()
	lastBuild, err := job.GetLastCompletedBuild(ctx)
	if err == nil && lastBuild != nil {
		fmt.Printf("%s最后构建: #%d (%s)\n", indent, lastBuild.GetBuildNumber(), lastBuild.GetResult())
	} else {
		fmt.Printf("%s最后构建: 无\n", indent)
	}
}
