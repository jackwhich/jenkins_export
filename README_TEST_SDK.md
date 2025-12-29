# Jenkins Job SDK 测试脚本

## 使用方法

### 1. 获取所有 job

```bash
go run test_job_sdk.go \
  -url http://jenkins.example.com \
  -user username \
  -pass password
```

### 2. 递归获取所有 job（包括文件夹下的）

```bash
go run test_job_sdk.go \
  -url http://jenkins.example.com \
  -user username \
  -pass password \
  -recursive
```

### 3. 获取指定文件夹下的所有 job

```bash
go run test_job_sdk.go \
  -url http://jenkins.example.com \
  -user username \
  -pass password \
  -folder prod-gray-ebpay \
  -recursive
```

### 4. 获取指定的 job

```bash
go run test_job_sdk.go \
  -url http://jenkins.example.com \
  -user username \
  -pass password \
  -job prod-gray-ebpay/gray-prod-mkt-thirdpart-api
```

### 5. 使用环境变量

```bash
export JENKINS_USERNAME=username
export JENKINS_PASSWORD=password

go run test_job_sdk.go \
  -url http://jenkins.example.com \
  -folder prod-gray-ebpay \
  -recursive
```

## 参数说明

- `-url`: Jenkins URL（必需）
- `-user`: Jenkins 用户名（可选，可从环境变量 JENKINS_USERNAME 获取）
- `-pass`: Jenkins 密码（可选，可从环境变量 JENKINS_PASSWORD 获取）
- `-folder`: 文件夹名称（可选）
- `-job`: 指定的 job 名称（可选）
- `-recursive`: 是否递归获取（默认 true）
- `-timeout`: 请求超时时间（默认 30s）

## 示例输出

```
连接到 Jenkins: http://jenkins.example.com
用户名: admin
✅ 成功连接到 Jenkins

获取文件夹下的 job: prod-gray-ebpay (递归: true)

文件夹信息:
  名称: prod-gray-ebpay
  类型: com.cloudbees.hudson.plugins.folder.Folder
  URL: http://jenkins.example.com/job/prod-gray-ebpay/

递归获取文件夹下的所有 job:
📁 文件夹: prod-gray-ebpay
  ✅ Job: prod-gray-ebpay/gray-prod-mkt-thirdpart-api
  ✅ Job: prod-gray-ebpay/gray-prod-mkt-tool-service
  ...

总共找到 145 个 job:
1. prod-gray-ebpay/gray-prod-mkt-thirdpart-api
2. prod-gray-ebpay/gray-prod-mkt-tool-service
...
```
