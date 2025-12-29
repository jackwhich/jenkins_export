package storage

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Job represents a job record in the database.
type Job struct {
	JobName       string
	Enabled       bool
	LastSeenBuild int64
	LastSyncTime  *time.Time
	CreatedAt     time.Time
}

// JobRepo provides methods for job data access.
type JobRepo struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewJobRepo creates a new JobRepo instance.
func NewJobRepo(db *sql.DB, logger *slog.Logger) *JobRepo {
	return &JobRepo{
		db:     db,
		logger: logger.With("component", "job_repo"),
	}
}

// ListEnabledJobs returns all enabled jobs from the database.
func (r *JobRepo) ListEnabledJobs() ([]Job, error) {
	query := `
		SELECT job_name, enabled, last_seen_build, last_sync_time, created_at
		FROM jobs
		WHERE enabled = 1
		ORDER BY job_name`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query enabled jobs: %w", err)
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var job Job
		var lastSyncTime, createdAt sql.NullInt64

		if err := rows.Scan(
			&job.JobName,
			&job.Enabled,
			&job.LastSeenBuild,
			&lastSyncTime,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}

		if lastSyncTime.Valid {
			t := time.Unix(lastSyncTime.Int64, 0)
			job.LastSyncTime = &t
		}

		if createdAt.Valid {
			job.CreatedAt = time.Unix(createdAt.Int64, 0)
		}

		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating jobs: %w", err)
	}

	return jobs, nil
}

// UpdateLastSeen updates the last_seen_build for a job.
func (r *JobRepo) UpdateLastSeen(jobName string, buildNumber int64) error {
	query := `
		UPDATE jobs
		SET last_seen_build = ?
		WHERE job_name = ?`

	result, err := r.db.Exec(query, buildNumber, jobName)
	if err != nil {
		return fmt.Errorf("failed to update last_seen_build: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		r.logger.Warn("更新 last_seen_build 时未找到对应的 job",
			"job_name", jobName,
		)
	}

	return nil
}

// SyncJobs synchronizes the job list with Jenkins.
// It adds new jobs, soft-deletes removed jobs, and updates last_sync_time for existing jobs.
func (r *JobRepo) SyncJobs(jobNames []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 创建 job 名称集合用于快速查找
	jobNameSet := make(map[string]bool, len(jobNames))
	for _, name := range jobNames {
		jobNameSet[name] = true
	}

	// 获取当前数据库中的所有 enabled=1 的 job
	existingJobs, err := r.listEnabledJobsInTx(tx)
	if err != nil {
		return fmt.Errorf("failed to list existing jobs: %w", err)
	}

	now := time.Now().Unix()
	addedCount := 0
	deletedCount := 0
	updatedCount := 0

	// 处理新增的 job
	for _, jobName := range jobNames {
		if !r.jobExistsInTx(tx, jobName) {
			insertQuery := `
				INSERT INTO jobs(job_name, enabled, last_seen_build, last_sync_time, created_at)
				VALUES (?, 1, 0, ?, ?)`

			if _, err := tx.Exec(insertQuery, jobName, now, now); err != nil {
				return fmt.Errorf("failed to insert job %s: %w", jobName, err)
			}

			// 记录审计日志
			if err := r.recordJobChange(tx, jobName, "ADD", now); err != nil {
				r.logger.Warn("记录 job 变更审计日志失败",
					"job_name", jobName,
					"action", "ADD",
					"error", err,
				)
			}

			addedCount++
		} else {
			// 更新 last_sync_time
			updateQuery := `
				UPDATE jobs
				SET last_sync_time = ?
				WHERE job_name = ?`

			if _, err := tx.Exec(updateQuery, now, jobName); err != nil {
				return fmt.Errorf("failed to update last_sync_time for %s: %w", jobName, err)
			}
			updatedCount++
		}
	}

	// 处理软删除的 job（在数据库中但不在 Jenkins 中）
	for _, existingJob := range existingJobs {
		if !jobNameSet[existingJob.JobName] {
			deleteQuery := `
				UPDATE jobs
				SET enabled = 0
				WHERE job_name = ?`

			if _, err := tx.Exec(deleteQuery, existingJob.JobName); err != nil {
				return fmt.Errorf("failed to soft delete job %s: %w", existingJob.JobName, err)
			}

			// 记录审计日志
			if err := r.recordJobChange(tx, existingJob.JobName, "DELETE", now); err != nil {
				r.logger.Warn("记录 job 变更审计日志失败",
					"job_name", existingJob.JobName,
					"action", "DELETE",
					"error", err,
				)
			}

			deletedCount++
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	r.logger.Info("Job 列表同步完成",
		"新增", addedCount,
		"删除", deletedCount,
		"更新", updatedCount,
		"总计", len(jobNames),
	)

	return nil
}

// listEnabledJobsInTx lists enabled jobs within a transaction.
func (r *JobRepo) listEnabledJobsInTx(tx *sql.Tx) ([]Job, error) {
	query := `SELECT job_name FROM jobs WHERE enabled = 1`

	rows, err := tx.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var job Job
		if err := rows.Scan(&job.JobName); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}

	return jobs, rows.Err()
}

// jobExistsInTx checks if a job exists in the database within a transaction.
func (r *JobRepo) jobExistsInTx(tx *sql.Tx, jobName string) bool {
	query := `SELECT 1 FROM jobs WHERE job_name = ? LIMIT 1`

	var exists int
	err := tx.QueryRow(query, jobName).Scan(&exists)
	return err == nil
}

// recordJobChange records a job change event in the audit table.
func (r *JobRepo) recordJobChange(tx *sql.Tx, jobName, action string, eventTime int64) error {
	query := `
		INSERT INTO job_changes(job_name, action, event_time)
		VALUES (?, ?, ?)`

	_, err := tx.Exec(query, jobName, action, eventTime)
	return err
}

