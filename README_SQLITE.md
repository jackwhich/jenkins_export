# SQLite 模式使用指南

## 快速开始

### 1. 基本配置（必须）

```bash
# Jenkins 连接信息（必须）
export JENKINS_EXPORTER_URL=http://jenkins.example.com
export JENKINS_EXPORTER_USERNAME=your_username
export JENKINS_EXPORTER_PASSWORD=your_password

# SQLite 数据库路径（必须）
export JENKINS_EXPORTER_COLLECTOR_JOBS_SQLITE_PATH=/var/lib/jenkins_exporter/jobs.db
```

### 2. 可选配置

```bash
# Discovery 同步间隔（默认 5 分钟）
export JENKINS_EXPORTER_COLLECTOR_JOBS_DISCOVERY_INTERVAL=5m

# Collector 采集间隔（默认 15 秒）
export JENKINS_EXPORTER_COLLECTOR_JOBS_COLLECTOR_INTERVAL=15s

# 指定文件夹（可选，为空则获取所有文件夹）
export JENKINS_EXPORTER_COLLECTOR_JOBS_FOLDERS=uat,pro,prod

# Web 服务地址（默认 0.0.0.0:9506）
export JENKINS_EXPORTER_WEB_ADDRESS=0.0.0.0:9506
```

### 3. 启动服务

```bash
./jenkins_exporter
```

## 完整示例

### 方式 1: 使用环境变量（推荐）

```bash
#!/bin/bash

# Jenkins 认证信息
export JENKINS_EXPORTER_URL=http://jenkins.example.com:8080
export JENKINS_EXPORTER_USERNAME=admin
export JENKINS_EXPORTER_PASSWORD=your_password_here

# SQLite 配置
export JENKINS_EXPORTER_COLLECTOR_JOBS_SQLITE_PATH=/var/lib/jenkins_exporter/jobs.db

# 可选：自定义间隔
export JENKINS_EXPORTER_COLLECTOR_JOBS_DISCOVERY_INTERVAL=5m
export JENKINS_EXPORTER_COLLECTOR_JOBS_COLLECTOR_INTERVAL=15s

# 启动
./jenkins_exporter
```

### 方式 2: 使用命令行参数

```bash
./jenkins_exporter \
  --jenkins.url=http://jenkins.example.com:8080 \
  --jenkins.username=admin \
  --jenkins.password=your_password_here \
  --collector.jobs.sqlite-path=/var/lib/jenkins_exporter/jobs.db \
  --collector.jobs.discovery-interval=5m \
  --collector.jobs.collector-interval=15s \
  --web.address=0.0.0.0:9506
```

### 方式 3: 使用文件存储密码（最安全）

```bash
# 创建密码文件
mkdir -p /etc/jenkins_exporter
echo "admin" > /etc/jenkins_exporter/username
echo "your_password_here" > /etc/jenkins_exporter/password
chmod 600 /etc/jenkins_exporter/username /etc/jenkins_exporter/password

# 使用 file:// 前缀
export JENKINS_EXPORTER_URL=http://jenkins.example.com:8080
export JENKINS_EXPORTER_USERNAME=file:///etc/jenkins_exporter/username
export JENKINS_EXPORTER_PASSWORD=file:///etc/jenkins_exporter/password
export JENKINS_EXPORTER_COLLECTOR_JOBS_SQLITE_PATH=/var/lib/jenkins_exporter/jobs.db

./jenkins_exporter
```

## Systemd 服务配置示例

创建 `/etc/systemd/system/jenkins-exporter.service`:

```ini
[Unit]
Description=Jenkins Exporter
After=network.target

[Service]
Type=simple
User=jenkins_exporter
Group=jenkins_exporter
WorkingDirectory=/opt/jenkins_exporter

# 环境变量
Environment="JENKINS_EXPORTER_URL=http://jenkins.example.com:8080"
Environment="JENKINS_EXPORTER_USERNAME=file:///etc/jenkins_exporter/username"
Environment="JENKINS_EXPORTER_PASSWORD=file:///etc/jenkins_exporter/password"
Environment="JENKINS_EXPORTER_COLLECTOR_JOBS_SQLITE_PATH=/var/lib/jenkins_exporter/jobs.db"
Environment="JENKINS_EXPORTER_COLLECTOR_JOBS_DISCOVERY_INTERVAL=5m"
Environment="JENKINS_EXPORTER_COLLECTOR_JOBS_COLLECTOR_INTERVAL=15s"
Environment="JENKINS_EXPORTER_WEB_ADDRESS=0.0.0.0:9506"

# 执行
ExecStart=/opt/jenkins_exporter/jenkins_exporter

# 重启策略
Restart=always
RestartSec=10

# 日志
StandardOutput=journal
StandardError=journal
SyslogIdentifier=jenkins_exporter

[Install]
WantedBy=multi-user.target
```

启动服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable jenkins-exporter
sudo systemctl start jenkins-exporter
sudo systemctl status jenkins-exporter
```

## 验证

### 1. 检查服务状态

```bash
curl http://localhost:9506/healthz
# 应该返回: OK
```

### 2. 查看指标

```bash
curl http://localhost:9506/metrics | grep jenkins_build_last_result
```

### 3. 检查 SQLite 数据库

```bash
sqlite3 /var/lib/jenkins_exporter/jobs.db "SELECT COUNT(*) FROM jobs WHERE enabled=1;"
```

## 常见问题

### Q: 如何查看所有配置项？

```bash
./jenkins_exporter --help
```

### Q: 如何查看版本？

```bash
./jenkins_exporter --version
```

### Q: SQLite 数据库文件在哪里？

默认路径由 `--collector.jobs.sqlite-path` 或 `JENKINS_EXPORTER_COLLECTOR_JOBS_SQLITE_PATH` 指定。

### Q: 如何备份 SQLite 数据库？

```bash
sqlite3 /var/lib/jenkins_exporter/jobs.db ".backup /backup/jobs.db.backup"
```

### Q: 如何迁移到 SQLite 模式？

如果之前使用 JSON 缓存模式，首次启动 SQLite 模式时会自动从 Jenkins 同步所有 job，无需手动迁移。

### Q: 如何查看日志？

如果使用 systemd，查看日志：

```bash
journalctl -u jenkins-exporter -f
```

## 性能调优

### 1. 调整采集间隔

- **Discovery 间隔**：建议 5-10 分钟（job 列表变化慢）
- **Collector 间隔**：建议 15-60 秒（根据需求调整）

### 2. SQLite 优化

SQLite 已自动配置：
- WAL 模式（提升并发性能）
- NORMAL 同步（性能与安全平衡）
- MEMORY 临时存储（减少磁盘 IO）
- 必要的索引

### 3. 限制文件夹范围

如果只需要监控特定文件夹：

```bash
export JENKINS_EXPORTER_COLLECTOR_JOBS_FOLDERS=uat,pro,prod
```

这样可以减少 Discovery 的 API 调用量。

