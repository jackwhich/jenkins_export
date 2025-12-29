# 测试脚本使用指南

## test_data.py - 数据测试脚本

用于测试 Jenkins Exporter 的 SQLite 数据库和 Prometheus 指标数据。

### 安装依赖

```bash
pip3 install requests
```

### 基本用法

```bash
# 测试默认路径的数据库和指标
python3 test_data.py

# 指定 SQLite 数据库路径
python3 test_data.py --sqlite-path /var/lib/jenkins_exporter/jobs.db

# 指定指标 URL
python3 test_data.py --metrics-url http://localhost:9506/metrics

# 组合使用
python3 test_data.py \
  --sqlite-path /var/lib/jenkins_exporter/jobs.db \
  --metrics-url http://localhost:9506/metrics
```

### 测试内容

脚本会测试以下内容：

1. **表结构测试**
   - 检查 jobs 表是否存在
   - 检查 job_changes 表是否存在
   - 检查索引是否正确创建

2. **Jobs 数据测试**
   - 统计总 job 数
   - 统计启用/禁用的 job
   - 显示前 10 个 job 的详细信息
   - 统计 last_seen_build 分布

3. **Job 变更测试**
   - 统计总变更记录数
   - 按操作类型统计（ADD/DELETE）
   - 显示最近的变更记录

4. **Prometheus 指标测试**
   - 检查指标服务是否可访问
   - 统计 jenkins_build_last_result 指标数量
   - 按状态统计构建结果
   - 显示示例指标

### 输出示例

```
============================================================
Jenkins Exporter 数据测试
============================================================
SQLite 路径: /var/lib/jenkins_exporter/jobs.db
指标 URL: http://localhost:9506/metrics
============================================================
✅ 成功连接到 SQLite 数据库: /var/lib/jenkins_exporter/jobs.db

📊 测试表结构...
  ✅ jobs 表存在
  ✅ job_changes 表存在
  ✅ 找到 4 个索引
     - idx_jobs_enabled
     - idx_jobs_enabled_lastseen
     - idx_jobs_last_sync_time
     - idx_job_changes_time

📋 测试 jobs 表数据...
  📊 总 job 数: 150
  ✅ 启用的 job: 145
  ❌ 禁用的 job: 5
  ...

📈 测试 Prometheus 指标...
  ✅ 成功获取指标 (大小: 45678 字节)
  📊 jenkins_build_last_result 指标数量: 145
  ...
```

### 测试报告

测试完成后会生成 JSON 格式的测试报告：

```json
{
  "timestamp": "2024-12-29T16:30:00",
  "sqlite_path": "/var/lib/jenkins_exporter/jobs.db",
  "metrics_url": "http://localhost:9506/metrics",
  "tests": {
    "enabled_jobs": 145,
    "disabled_jobs": 5,
    "total_changes": 200,
    "metrics_count": 145
  }
}
```

### 常见问题

**Q: 提示无法连接数据库？**

A: 检查数据库路径是否正确，以及是否有读取权限：
```bash
ls -l /var/lib/jenkins_exporter/jobs.db
```

**Q: 提示无法连接指标服务？**

A: 确保 jenkins_exporter 正在运行：
```bash
curl http://localhost:9506/healthz
```

**Q: 如何查看详细的测试报告？**

A: 测试完成后会生成 `test_report_YYYYMMDD_HHMMSS.json` 文件，可以用任何文本编辑器查看。

### 集成到 CI/CD

可以在 CI/CD 流程中使用：

```bash
#!/bin/bash
# 运行测试
python3 test_data.py \
  --sqlite-path /var/lib/jenkins_exporter/jobs.db \
  --metrics-url http://localhost:9506/metrics

# 检查退出码
if [ $? -eq 0 ]; then
    echo "✅ 所有测试通过"
else
    echo "❌ 测试失败"
    exit 1
fi
```

