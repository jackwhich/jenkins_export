#!/usr/bin/env python3
"""
Jenkins Exporter 数据测试脚本

用于测试 SQLite 数据库和 Prometheus 指标数据
"""

import sqlite3
import requests
import json
import sys
from datetime import datetime
from typing import Dict, List, Optional


class JenkinsExporterTester:
    def __init__(self, sqlite_path: str, metrics_url: str = "http://localhost:9506/metrics"):
        """
        初始化测试器
        
        Args:
            sqlite_path: SQLite 数据库路径
            metrics_url: Prometheus 指标 URL
        """
        self.sqlite_path = sqlite_path
        self.metrics_url = metrics_url
        self.conn = None

    def connect_db(self) -> bool:
        """连接 SQLite 数据库"""
        try:
            self.conn = sqlite3.connect(self.sqlite_path)
            self.conn.row_factory = sqlite3.Row
            print(f"✅ 成功连接到 SQLite 数据库: {self.sqlite_path}")
            return True
        except Exception as e:
            print(f"❌ 连接数据库失败: {e}")
            return False

    def test_tables(self) -> bool:
        """测试表结构"""
        print("\n📊 测试表结构...")
        try:
            cursor = self.conn.cursor()
            
            # 检查 jobs 表
            cursor.execute("""
                SELECT name FROM sqlite_master 
                WHERE type='table' AND name='jobs'
            """)
            if cursor.fetchone():
                print("  ✅ jobs 表存在")
            else:
                print("  ❌ jobs 表不存在")
                return False

            # 检查 job_changes 表
            cursor.execute("""
                SELECT name FROM sqlite_master 
                WHERE type='table' AND name='job_changes'
            """)
            if cursor.fetchone():
                print("  ✅ job_changes 表存在")
            else:
                print("  ⚠️  job_changes 表不存在（可选）")

            # 检查索引
            cursor.execute("""
                SELECT name FROM sqlite_master 
                WHERE type='index' AND name LIKE 'idx_%'
            """)
            indexes = cursor.fetchall()
            print(f"  ✅ 找到 {len(indexes)} 个索引")
            for idx in indexes:
                print(f"     - {idx[0]}")

            return True
        except Exception as e:
            print(f"  ❌ 测试表结构失败: {e}")
            return False

    def test_jobs_data(self) -> bool:
        """测试 jobs 表数据"""
        print("\n📋 测试 jobs 表数据...")
        try:
            cursor = self.conn.cursor()

            # 统计总数
            cursor.execute("SELECT COUNT(*) FROM jobs")
            total = cursor.fetchone()[0]
            print(f"  📊 总 job 数: {total}")

            # 统计启用的 job
            cursor.execute("SELECT COUNT(*) FROM jobs WHERE enabled = 1")
            enabled = cursor.fetchone()[0]
            print(f"  ✅ 启用的 job: {enabled}")

            # 统计禁用的 job
            cursor.execute("SELECT COUNT(*) FROM jobs WHERE enabled = 0")
            disabled = cursor.fetchone()[0]
            print(f"  ❌ 禁用的 job: {disabled}")

            # 显示前 10 个 job
            cursor.execute("""
                SELECT job_name, enabled, last_seen_build, 
                       datetime(last_sync_time, 'unixepoch') as sync_time,
                       datetime(created_at, 'unixepoch') as created
                FROM jobs 
                WHERE enabled = 1
                ORDER BY created_at DESC
                LIMIT 10
            """)
            jobs = cursor.fetchall()
            
            if jobs:
                print("\n  📝 前 10 个启用的 job:")
                for job in jobs:
                    print(f"     - {job['job_name']}")
                    print(f"       状态: {'启用' if job['enabled'] else '禁用'}")
                    print(f"       最后构建: {job['last_seen_build']}")
                    print(f"       同步时间: {job['sync_time']}")
                    print(f"       创建时间: {job['created']}")
                    print()

            # 统计 last_seen_build 分布
            cursor.execute("""
                SELECT 
                    CASE 
                        WHEN last_seen_build = 0 THEN '0 (未处理)'
                        WHEN last_seen_build < 10 THEN '1-9'
                        WHEN last_seen_build < 100 THEN '10-99'
                        ELSE '100+'
                    END as build_range,
                    COUNT(*) as count
                FROM jobs
                WHERE enabled = 1
                GROUP BY build_range
                ORDER BY build_range
            """)
            ranges = cursor.fetchall()
            if ranges:
                print("  📊 last_seen_build 分布:")
                for r in ranges:
                    print(f"     {r['build_range']}: {r['count']} 个 job")

            return True
        except Exception as e:
            print(f"  ❌ 测试 jobs 数据失败: {e}")
            return False

    def test_job_changes(self) -> bool:
        """测试 job_changes 审计表"""
        print("\n📝 测试 job_changes 审计表...")
        try:
            cursor = self.conn.cursor()

            # 统计总数
            cursor.execute("SELECT COUNT(*) FROM job_changes")
            total = cursor.fetchone()[0]
            print(f"  📊 总变更记录: {total}")

            if total > 0:
                # 按操作类型统计
                cursor.execute("""
                    SELECT action, COUNT(*) as count
                    FROM job_changes
                    GROUP BY action
                """)
                actions = cursor.fetchall()
                print("  📊 操作类型统计:")
                for action in actions:
                    print(f"     {action['action']}: {action['count']} 次")

                # 显示最近的变更
                cursor.execute("""
                    SELECT job_name, action, 
                           datetime(event_time, 'unixepoch') as event_time
                    FROM job_changes
                    ORDER BY event_time DESC
                    LIMIT 10
                """)
                changes = cursor.fetchall()
                print("\n  📝 最近 10 次变更:")
                for change in changes:
                    print(f"     [{change['event_time']}] {change['action']}: {change['job_name']}")

            return True
        except Exception as e:
            print(f"  ❌ 测试 job_changes 失败: {e}")
            return False

    def test_metrics(self) -> bool:
        """测试 Prometheus 指标"""
        print("\n📈 测试 Prometheus 指标...")
        try:
            response = requests.get(self.metrics_url, timeout=5)
            if response.status_code != 200:
                print(f"  ❌ 获取指标失败: HTTP {response.status_code}")
                return False

            print(f"  ✅ 成功获取指标 (大小: {len(response.text)} 字节)")

            # 解析指标
            lines = response.text.split('\n')
            metrics = {}
            for line in lines:
                if line.startswith('jenkins_build_last_result'):
                    # 解析指标行
                    parts = line.split(' ')
                    if len(parts) >= 2:
                        metric_name = parts[0]
                        value = parts[1]
                        metrics[metric_name] = value

            # 统计指标
            build_result_count = sum(1 for line in lines if 'jenkins_build_last_result' in line and not line.startswith('#'))
            print(f"  📊 jenkins_build_last_result 指标数量: {build_result_count}")

            # 按状态统计
            status_count = {}
            for line in lines:
                if 'jenkins_build_last_result' in line and 'status=' in line:
                    # 提取 status 值
                    try:
                        status_part = [p for p in line.split(' ') if 'status=' in p][0]
                        status = status_part.split('=')[1].strip('"')
                        status_count[status] = status_count.get(status, 0) + 1
                    except:
                        pass

            if status_count:
                print("\n  📊 构建状态分布:")
                for status, count in sorted(status_count.items()):
                    print(f"     {status}: {count}")

            # 显示一些示例指标
            print("\n  📝 示例指标 (前 5 个):")
            count = 0
            for line in lines:
                if 'jenkins_build_last_result' in line and not line.startswith('#') and count < 5:
                    print(f"     {line[:100]}...")
                    count += 1

            return True
        except requests.exceptions.ConnectionError:
            print(f"  ⚠️  无法连接到指标服务: {self.metrics_url}")
            print("     请确保 jenkins_exporter 正在运行")
            return False
        except Exception as e:
            print(f"  ❌ 测试指标失败: {e}")
            return False

    def generate_test_report(self) -> Dict:
        """生成测试报告"""
        report = {
            "timestamp": datetime.now().isoformat(),
            "sqlite_path": self.sqlite_path,
            "metrics_url": self.metrics_url,
            "tests": {}
        }

        # 测试数据库
        if self.conn:
            cursor = self.conn.cursor()
            
            # jobs 统计
            cursor.execute("SELECT COUNT(*) FROM jobs WHERE enabled = 1")
            report["tests"]["enabled_jobs"] = cursor.fetchone()[0]
            
            cursor.execute("SELECT COUNT(*) FROM jobs WHERE enabled = 0")
            report["tests"]["disabled_jobs"] = cursor.fetchone()[0]
            
            # 变更统计
            cursor.execute("SELECT COUNT(*) FROM job_changes")
            report["tests"]["total_changes"] = cursor.fetchone()[0]

        # 测试指标
        try:
            response = requests.get(self.metrics_url, timeout=5)
            if response.status_code == 200:
                lines = response.text.split('\n')
                report["tests"]["metrics_count"] = sum(
                    1 for line in lines 
                    if 'jenkins_build_last_result' in line and not line.startswith('#')
                )
        except:
            report["tests"]["metrics_count"] = None

        return report

    def run_all_tests(self) -> bool:
        """运行所有测试"""
        print("=" * 60)
        print("Jenkins Exporter 数据测试")
        print("=" * 60)
        print(f"SQLite 路径: {self.sqlite_path}")
        print(f"指标 URL: {self.metrics_url}")
        print("=" * 60)

        if not self.connect_db():
            return False

        results = []
        results.append(("表结构", self.test_tables()))
        results.append(("Jobs 数据", self.test_jobs_data()))
        results.append(("Job 变更", self.test_job_changes()))
        results.append(("Prometheus 指标", self.test_metrics()))

        print("\n" + "=" * 60)
        print("测试结果汇总:")
        print("=" * 60)
        for name, result in results:
            status = "✅ 通过" if result else "❌ 失败"
            print(f"  {name}: {status}")

        all_passed = all(result for _, result in results)
        print("=" * 60)

        # 生成报告
        report = self.generate_test_report()
        report_file = f"test_report_{datetime.now().strftime('%Y%m%d_%H%M%S')}.json"
        with open(report_file, 'w') as f:
            json.dump(report, f, indent=2, ensure_ascii=False)
        print(f"\n📄 测试报告已保存: {report_file}")

        return all_passed

    def close(self):
        """关闭数据库连接"""
        if self.conn:
            self.conn.close()


def main():
    """主函数"""
    import argparse

    parser = argparse.ArgumentParser(description="Jenkins Exporter 数据测试脚本")
    parser.add_argument(
        "--sqlite-path",
        default="/var/lib/jenkins_exporter/jobs.db",
        help="SQLite 数据库路径 (默认: /var/lib/jenkins_exporter/jobs.db)"
    )
    parser.add_argument(
        "--metrics-url",
        default="http://localhost:9506/metrics",
        help="Prometheus 指标 URL (默认: http://localhost:9506/metrics)"
    )
    parser.add_argument(
        "--create-sample",
        action="store_true",
        help="创建示例数据（用于测试）"
    )

    args = parser.parse_args()

    tester = JenkinsExporterTester(args.sqlite_path, args.metrics_url)

    if args.create_sample:
        # 创建示例数据
        print("创建示例数据...")
        if tester.connect_db():
            cursor = tester.conn.cursor()
            now = int(datetime.now().timestamp())
            
            # 插入示例 jobs
            sample_jobs = [
                ("test/job1", 1, 10, now, now),
                ("test/job2", 1, 25, now, now),
                ("test/job3", 1, 0, now, now),
                ("deleted/job4", 0, 5, now - 3600, now - 3600),
            ]
            
            cursor.executemany("""
                INSERT OR REPLACE INTO jobs 
                (job_name, enabled, last_seen_build, last_sync_time, created_at)
                VALUES (?, ?, ?, ?, ?)
            """, sample_jobs)
            
            # 插入示例变更
            sample_changes = [
                ("test/job1", "ADD", now),
                ("test/job2", "ADD", now),
                ("deleted/job4", "DELETE", now - 3600),
            ]
            
            cursor.executemany("""
                INSERT INTO job_changes (job_name, action, event_time)
                VALUES (?, ?, ?)
            """, sample_changes)
            
            tester.conn.commit()
            print("✅ 示例数据创建完成")
            tester.close()
            return

    # 运行测试
    try:
        success = tester.run_all_tests()
        sys.exit(0 if success else 1)
    finally:
        tester.close()


if __name__ == "__main__":
    main()

