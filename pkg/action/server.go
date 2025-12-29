package action

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/promhippie/jenkins_exporter/pkg/config"
	"github.com/promhippie/jenkins_exporter/pkg/exporter"
	"github.com/promhippie/jenkins_exporter/pkg/internal/jenkins"
	"github.com/promhippie/jenkins_exporter/pkg/internal/storage"
	"github.com/promhippie/jenkins_exporter/pkg/middleware"
	"github.com/promhippie/jenkins_exporter/pkg/version"
)

// Server handles the server sub-command.
func Server(cfg *config.Config, logger *slog.Logger) error {
	logger.Info("正在启动 Jenkins Exporter",
		"版本", version.String,
		"修订", version.Revision,
		"日期", version.Date,
		"Go版本", version.Go,
	)

	username, err := config.Value(cfg.Target.Username)

	if err != nil {
		logger.Error("从文件加载用户名失败",
			"错误", err,
		)

		return err
	}

	password, err := config.Value(cfg.Target.Password)

	if err != nil {
		logger.Error("从文件加载密码失败",
			"错误", err,
		)

		return err
	}

	logger.Info("正在连接 Jenkins",
		"address", cfg.Target.Address,
		"timeout", cfg.Target.Timeout,
	)

	client, err := jenkins.NewClient(
		jenkins.WithEndpoint(cfg.Target.Address),
		jenkins.WithUsername(username),
		jenkins.WithPassword(password),
		jenkins.WithTimeout(cfg.Target.Timeout),
	)

	if err != nil {
		logger.Error("连接 Jenkins 失败",
			"address", cfg.Target.Address,
			"err", err,
		)

		return err
	}

	logger.Info("成功连接到 Jenkins",
		"address", cfg.Target.Address,
	)

	var gr run.Group
	var jobCollector *exporter.JobCollector
	var buildCollector *jenkins.BuildCollector
	var jobRepo *storage.JobRepo

	// 如果启用了 SQLite，使用 SQLite 模式（推荐）
	if cfg.Collector.Jobs && cfg.Collector.SQLitePath != "" {
		logger.Info("正在初始化 SQLite 数据库",
			"数据库路径", cfg.Collector.SQLitePath,
		)

		db, err := storage.NewSQLite(cfg.Collector.SQLitePath, logger)
		if err != nil {
			logger.Error("初始化 SQLite 数据库失败",
				"错误", err,
			)
			return err
		}

		jobRepo = storage.NewJobRepo(db, logger)

		// 解析文件夹列表
		folders := jenkins.GetJobNamesFromFolders(cfg.Collector.FoldersStr)

		// 启动 Job Discovery（低频同步）
		discoveryCtx, discoveryCancel := context.WithCancel(context.Background())
		gr.Add(func() error {
			return jenkins.StartDiscovery(
				discoveryCtx,
				client,
				jobRepo,
				cfg.Collector.DiscoveryInterval,
				folders,
				logger,
			)
		}, func(_ error) {
			discoveryCancel()
		})

		// 创建并启动 Build Collector（高频采集）
		buildCollector = jenkins.NewBuildCollector(client, jobRepo, logger)
		collectorCtx, collectorCancel := context.WithCancel(context.Background())
		gr.Add(func() error {
			return buildCollector.Start(collectorCtx, cfg.Collector.CollectorInterval)
		}, func(_ error) {
			collectorCancel()
		})

		logger.Info("SQLite 模式已启用",
			"Discovery 间隔", cfg.Collector.DiscoveryInterval,
			"Collector 间隔", cfg.Collector.CollectorInterval,
		)
	} else if cfg.Collector.Jobs {
		// 传统模式：使用 JSON 缓存（不推荐，仅用于兼容）
		logger.Info("使用传统模式（JSON 缓存），建议使用 SQLite 模式以获得更好的性能",
			"提示", "设置 --collector.jobs.sqlite-path 启用 SQLite 模式",
		)
		// 解析逗号分隔的文件夹字符串
		var folders []string
		if cfg.Collector.FoldersStr != "" {
			parts := strings.Split(cfg.Collector.FoldersStr, ",")
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					folders = append(folders, trimmed)
				}
			}
		}

		jobCollector = exporter.NewJobCollector(
			logger,
			client,
			requestFailures,
			requestDuration,
			cfg.Target,
			cfg.Collector.FetchBuildDetails,
			cfg.Collector.CacheFile,
			cfg.Collector.CacheTTL,
			cfg.Collector.CacheRefreshInterval,
			folders,
		)

		// 在启动时初始化缓存文件
		if cfg.Collector.CacheFile != "" {
			logger.Info("正在初始化缓存文件",
				"缓存文件", cfg.Collector.CacheFile,
			)

			initCtx, initCancel := context.WithTimeout(context.Background(), cfg.Target.Timeout)
			if err := jobCollector.InitializeCache(initCtx); err != nil {
				logger.Warn("初始化缓存文件失败，将在首次请求时创建",
					"缓存文件", cfg.Collector.CacheFile,
					"错误", err,
				)
			}
			initCancel()
		}

		// 如果启用了定时刷新，启动定时刷新任务
		if cfg.Collector.CacheFile != "" && cfg.Collector.CacheRefreshInterval > 0 {
			refreshCtx, refreshCancel := context.WithCancel(context.Background())

			gr.Add(func() error {
				return jobCollector.StartCacheRefresh(refreshCtx)
			}, func(_ error) {
				refreshCancel()
				jobCollector.StopCacheRefresh()
			})
		}
	}

	{
		server := &http.Server{
			Addr:         cfg.Server.Addr,
			Handler:      handler(cfg, logger, client, jobCollector, buildCollector),
			ReadTimeout:  5 * time.Second,
			WriteTimeout: cfg.Server.Timeout,
		}

		gr.Add(func() error {
			logger.Info("正在启动指标服务器",
				"监听地址", cfg.Server.Addr,
			)

			return web.ListenAndServe(
				server,
				&web.FlagConfig{
					WebListenAddresses: sliceP([]string{cfg.Server.Addr}),
					WebSystemdSocket:   boolP(false),
					WebConfigFile:      stringP(cfg.Server.Web),
				},
				logger,
			)
		}, func(reason error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := server.Shutdown(ctx); err != nil {
				logger.Error("指标服务器优雅关闭失败",
					"错误", err,
				)

				return
			}

			logger.Info("指标服务器已优雅关闭",
				"原因", reason,
			)
		})
	}

	{
		stop := make(chan os.Signal, 1)

		gr.Add(func() error {
			signal.Notify(stop, os.Interrupt)

			<-stop

			return nil
		}, func(_ error) {
			close(stop)
		})
	}

	return gr.Run()
}

func handler(cfg *config.Config, logger *slog.Logger, client *jenkins.Client, jobCollector *exporter.JobCollector, buildCollector *jenkins.BuildCollector) *chi.Mux {
	mux := chi.NewRouter()
	mux.Use(middleware.Recoverer(logger))
	mux.Use(middleware.RealIP)
	mux.Use(middleware.Timeout)
	mux.Use(middleware.Cache)

	if cfg.Server.Pprof {
		mux.Mount("/debug", middleware.Profiler())
	}

	// 如果使用 SQLite 模式，注册 Build Collector
	if buildCollector != nil {
		logger.Info("已注册 Build Collector（SQLite 模式）")
		registry.MustRegister(buildCollector)
	}

	// 如果使用传统模式，注册 JobCollector（仅当未使用 SQLite 时）
	if cfg.Collector.Jobs && jobCollector != nil && cfg.Collector.SQLitePath == "" {
		// 解析逗号分隔的文件夹字符串（用于日志）
		var folders []string
		if cfg.Collector.FoldersStr != "" {
			parts := strings.Split(cfg.Collector.FoldersStr, ",")
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					folders = append(folders, trimmed)
				}
			}
		}

		if len(folders) > 0 {
			logger.Info("已注册作业收集器（传统模式）",
				"获取构建详情", cfg.Collector.FetchBuildDetails,
				"指定文件夹", folders,
			)
		} else {
			logger.Info("已注册作业收集器（传统模式）",
				"获取构建详情", cfg.Collector.FetchBuildDetails,
				"说明", "将获取所有文件夹下的作业",
			)
		}

		registry.MustRegister(jobCollector)
	}

	reg := promhttp.HandlerFor(
		registry,
		promhttp.HandlerOpts{
			ErrorLog: promLogger{logger},
		},
	)

	mux.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, cfg.Server.Path, http.StatusMovedPermanently)
	})

	mux.Route("/", func(root chi.Router) {
		root.Get(cfg.Server.Path, func(w http.ResponseWriter, r *http.Request) {
			reg.ServeHTTP(w, r)
		})

		root.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)

			_, _ = io.WriteString(w, http.StatusText(http.StatusOK))
		})

		root.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)

			_, _ = io.WriteString(w, http.StatusText(http.StatusOK))
		})
	})

	return mux
}
