package helper

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/raids-lab/crater/internal/handler/operations"

	"github.com/raids-lab/crater/internal"
	"github.com/raids-lab/crater/internal/handler"
	"github.com/raids-lab/crater/pkg/config"
)

// ServerRunner 封装服务器运行逻辑
type ServerRunner struct {
	backendConfig *config.Config
	Server        *http.Server
}

var (
	ServerRunnerInstance *ServerRunner
	once                 sync.Once
)

// NewServerRunner 创建新的ServerRunner实例
func NewServerRunner(backendConfig *config.Config) *ServerRunner {
	once.Do(func() {
		ServerRunnerInstance = &ServerRunner{
			backendConfig: backendConfig,
		}
	})
	if ServerRunnerInstance == nil {
		klog.Fatal("failed to create server runner")
	}
	return ServerRunnerInstance
}

// SetupLogger 设置日志记录器
func (sr *ServerRunner) SetupLogger() {
	ctrl.SetLogger(klog.NewKlogr())
}

// StartManager 启动管理器
func (sr *ServerRunner) StartManager(ctx context.Context, mgr manager.Manager) {
	klog.Info("starting manager")
	go func() {
		startErr := mgr.Start(ctx)
		if startErr != nil {
			klog.Error(startErr, "problem running manager")
			os.Exit(1)
		}
	}()

	mgr.GetCache().WaitForCacheSync(ctx)
	klog.Info("cache sync success")
}

var (
	readHeaderTimeout = 10 * time.Second // 设置读取头部的超时时间
	cancelTimeout     = 10 * time.Second // 设置取消操作的超时时间
)

func GetServerRunner() *ServerRunner {
	return ServerRunnerInstance
}

func (sr *ServerRunner) GetServerHandler() http.Handler {
	return sr.Server.Handler
}

// StartServer 启动HTTP服务器
func (sr *ServerRunner) StartServer(registerConfig *handler.RegisterConfig) {
	klog.Info("starting server")
	backend := internal.Register(registerConfig)

	// Set server handler to CronJobManager for in-process HTTP calls
	if operationManager := operations.GetOperationsMgrInstance(); operationManager != nil {
		operationManager.InitServerHandler(backend)
		klog.Info("Set server handler to OperationManager for in-process HTTP calls")
	}

	// reference: https://gin-gonic.com/en/docs/examples/graceful-restart-or-stop
	sr.Server = &http.Server{
		Addr:              sr.backendConfig.Port,
		Handler:           backend,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	go func() {
		// service connections
		if err := sr.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			klog.Fatalf("listen: %s\n", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server with
	// a timeout of 5 seconds.
	quit := make(chan os.Signal, 1)
	// kill (no params) by default sends syscall.SIGTERM
	// kill -2 is syscall.SIGINT
	// kill -9 is syscall.SIGKILL but can't be caught, so don't need add it
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	klog.Info("Shutdown Gin Server ...")

	ctx, cancel := context.WithTimeout(context.Background(), cancelTimeout)
	defer cancel()
	if err := sr.Server.Shutdown(ctx); err != nil {
		klog.Info("Gin Server Shutdown:", err)
	}
	klog.Info("Gin Server exiting")
}

// SetupHealthChecks 设置健康检查
func (sr *ServerRunner) SetupHealthChecks(mgr manager.Manager) {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}
}
