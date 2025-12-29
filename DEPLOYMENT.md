# 部署指南

## 快速部署步骤

### 1. 编译 Linux 版本

```bash
# 在 macOS 上交叉编译
cd jenkins_exporter
./build_linux.sh amd64

# 或在 Linux 服务器上直接编译
GOOS=linux GOARCH=amd64 go build -o jenkins_exporter_linux ./cmd/jenkins_exporter
```

### 2. 准备配置文件

创建配置文件目录：

```bash
sudo mkdir -p /etc/jenkins_exporter
sudo mkdir -p /var/lib/jenkins_exporter
sudo chown jenkins_exporter:jenkins_exporter /var/lib/jenkins_exporter
```

创建认证文件：

```bash
# 创建用户名文件
echo "your_jenkins_username" | sudo tee /etc/jenkins_exporter/username

# 创建密码文件
echo "your_jenkins_password" | sudo tee /etc/jenkins_exporter/password

# 设置权限
sudo chmod 600 /etc/jenkins_exporter/username /etc/jenkins_exporter/password
```

### 3. 创建 Systemd 服务

创建 `/etc/systemd/system/jenkins-exporter.service`:

```ini
[Unit]
Description=Jenkins Exporter
Documentation=https://github.com/promhippie/jenkins_exporter
After=network.target

[Service]
Type=simple
User=jenkins_exporter
Group=jenkins_exporter
WorkingDirectory=/opt/jenkins_exporter

# Jenkins 连接配置
Environment="JENKINS_EXPORTER_URL=http://jenkins.example.com:8080"
Environment="JENKINS_EXPORTER_USERNAME=file:///etc/jenkins_exporter/username"
Environment="JENKINS_EXPORTER_PASSWORD=file:///etc/jenkins_exporter/password"

# SQLite 配置
Environment="JENKINS_EXPORTER_COLLECTOR_JOBS_SQLITE_PATH=/var/lib/jenkins_exporter/jobs.db"
Environment="JENKINS_EXPORTER_COLLECTOR_JOBS_DISCOVERY_INTERVAL=5m"
Environment="JENKINS_EXPORTER_COLLECTOR_JOBS_COLLECTOR_INTERVAL=15s"

# Web 服务配置
Environment="JENKINS_EXPORTER_WEB_ADDRESS=0.0.0.0:9506"
Environment="JENKINS_EXPORTER_WEB_PATH=/metrics"

# 日志配置
Environment="JENKINS_EXPORTER_LOG_LEVEL=info"

# 执行
ExecStart=/opt/jenkins_exporter/jenkins_exporter

# 重启策略
Restart=always
RestartSec=10

# 资源限制
LimitNOFILE=65536

# 日志
StandardOutput=journal
StandardError=journal
SyslogIdentifier=jenkins_exporter

[Install]
WantedBy=multi-user.target
```

### 4. 部署二进制文件

```bash
# 创建用户（如果不存在）
sudo useradd -r -s /bin/false jenkins_exporter

# 复制二进制文件
sudo cp jenkins_exporter_linux /opt/jenkins_exporter/jenkins_exporter
sudo chmod +x /opt/jenkins_exporter/jenkins_exporter
sudo chown jenkins_exporter:jenkins_exporter /opt/jenkins_exporter/jenkins_exporter
```

### 5. 启动服务

```bash
# 重新加载 systemd
sudo systemctl daemon-reload

# 启用服务（开机自启）
sudo systemctl enable jenkins-exporter

# 启动服务
sudo systemctl start jenkins-exporter

# 查看状态
sudo systemctl status jenkins-exporter

# 查看日志
sudo journalctl -u jenkins-exporter -f
```

### 6. 验证部署

```bash
# 检查健康状态
curl http://localhost:9506/healthz

# 检查指标
curl http://localhost:9506/metrics | head -20

# 检查 SQLite 数据库
sqlite3 /var/lib/jenkins_exporter/jobs.db "SELECT COUNT(*) FROM jobs WHERE enabled=1;"
```

## Prometheus 配置

在 Prometheus 配置文件中添加：

```yaml
scrape_configs:
  - job_name: 'jenkins_exporter'
    static_configs:
      - targets: ['localhost:9506']
    metrics_path: '/metrics'
    scrape_interval: 30s
```

## Grafana 仪表板

可以使用以下 PromQL 查询：

```promql
# 构建失败告警
jenkins_build_last_result{status="failure"} == 1

# 指定分支失败
jenkins_build_last_result{status="failure", gitBranch="master"} == 1

# 统计各状态数量
count by (status) (jenkins_build_last_result == 1)
```

## 故障排查

### 查看日志

```bash
# 实时日志
sudo journalctl -u jenkins-exporter -f

# 最近 100 行
sudo journalctl -u jenkins-exporter -n 100

# 查看错误
sudo journalctl -u jenkins-exporter -p err
```

### 常见问题

1. **无法连接 Jenkins**
   - 检查 URL 是否正确
   - 检查用户名和密码是否正确
   - 检查网络连接

2. **SQLite 数据库权限问题**
   - 确保 `/var/lib/jenkins_exporter` 目录存在
   - 确保 `jenkins_exporter` 用户有写权限

3. **指标未更新**
   - 检查 Discovery 和 Collector 是否正常运行
   - 查看日志确认是否有错误

## 备份和恢复

### 备份 SQLite 数据库

```bash
# 备份
sqlite3 /var/lib/jenkins_exporter/jobs.db ".backup /backup/jobs_$(date +%Y%m%d).db"

# 或使用 cp（需要停止服务）
sudo systemctl stop jenkins-exporter
sudo cp /var/lib/jenkins_exporter/jobs.db /backup/jobs_$(date +%Y%m%d).db
sudo systemctl start jenkins-exporter
```

### 恢复数据库

```bash
# 停止服务
sudo systemctl stop jenkins-exporter

# 恢复数据库
sudo cp /backup/jobs_20231229.db /var/lib/jenkins_exporter/jobs.db
sudo chown jenkins_exporter:jenkins_exporter /var/lib/jenkins_exporter/jobs.db

# 启动服务
sudo systemctl start jenkins-exporter
```

## 升级

```bash
# 1. 备份数据库
sudo systemctl stop jenkins-exporter
sudo cp /var/lib/jenkins_exporter/jobs.db /backup/jobs_$(date +%Y%m%d).db

# 2. 替换二进制文件
sudo cp jenkins_exporter_linux_new /opt/jenkins_exporter/jenkins_exporter
sudo chmod +x /opt/jenkins_exporter/jenkins_exporter

# 3. 启动服务
sudo systemctl start jenkins-exporter
sudo systemctl status jenkins-exporter
```

