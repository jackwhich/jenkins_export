package action

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/promhippie/jenkins_exporter/pkg/config"
	"github.com/promhippie/jenkins_exporter/pkg/exporter"
	"github.com/promhippie/jenkins_exporter/pkg/internal/jenkins"
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

	{
		server := &http.Server{
			Addr:         cfg.Server.Addr,
			Handler:      handler(cfg, logger, client),
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

func handler(cfg *config.Config, logger *slog.Logger, client *jenkins.Client) *chi.Mux {
	mux := chi.NewRouter()
	mux.Use(middleware.Recoverer(logger))
	mux.Use(middleware.RealIP)
	mux.Use(middleware.Timeout)
	mux.Use(middleware.Cache)

	if cfg.Server.Pprof {
		mux.Mount("/debug", middleware.Profiler())
	}

	if cfg.Collector.Jobs {
		logger.Info("已注册作业收集器",
			"获取构建详情", cfg.Collector.FetchBuildDetails,
		)

		registry.MustRegister(exporter.NewJobCollector(
			logger,
			client,
			requestFailures,
			requestDuration,
			cfg.Target,
			cfg.Collector.FetchBuildDetails,
		))
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
