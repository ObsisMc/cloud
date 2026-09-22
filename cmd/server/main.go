package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/wanglongan587/cloud/internal/api/router"
	"github.com/wanglongan587/cloud/internal/config"
	"github.com/wanglongan587/cloud/internal/controlgrpc"
	"github.com/wanglongan587/cloud/internal/core"
	"github.com/wanglongan587/cloud/internal/logger"
	"github.com/wanglongan587/cloud/internal/repository"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

func run() (runErr error) {
	configPath := flag.String("config", "", "configuration file")
	flag.Parse()
	cfg, e := config.Load(*configPath)
	if e != nil {
		return e
	}
	log, e := logger.New(cfg.Logger)
	if e != nil {
		return e
	}
	defer func() { runErr = errors.Join(runErr, logger.Sync(log)) }()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	db, e := repository.InitDB(ctx, cfg.Database)
	if e != nil {
		return e
	}
	store, e := core.NewStore(db)
	if e != nil {
		return e
	}
	defer func() { runErr = errors.Join(runErr, store.Pool.Close()) }()
	if e := store.CheckSchema(ctx); e != nil {
		return e
	}
	auth, e := core.NewAuthenticator(cfg.Auth.Audience, cfg.Auth.Keys)
	if e != nil {
		return e
	}
	gin.SetMode(cfg.Server.Mode)
	server := &http.Server{Addr: fmt.Sprintf(":%d", cfg.Server.Port), Handler: router.New(store, auth, log), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: cfg.Server.ReadTimeout, WriteTimeout: cfg.Server.WriteTimeout, IdleTimeout: 60 * time.Second}
	// The control listener is bound before serving so a taken port fails startup, not a Controller.
	control, e := net.Listen("tcp", cfg.Control.GRPCAddr)
	if e != nil {
		return e
	}
	grpcServer := controlgrpc.New(store, auth, log)
	failed := make(chan error, 2)
	go func() {
		log.Info("Cloud listening", zap.String("address", server.Addr))
		failed <- server.ListenAndServe()
	}()
	go func() {
		log.Info("Cloud control listening", zap.String("address", control.Addr().String()))
		failed <- grpcServer.Serve(control)
	}()
	select {
	case e = <-failed:
		if errors.Is(e, http.ErrServerClosed) || errors.Is(e, grpc.ErrServerStopped) {
			return nil
		}
		return e
	case <-ctx.Done():
		shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		// In-flight control calls finish or are cut at the same deadline as HTTP; a Controller
		// retries with the same submission identity, so cutting them loses nothing durable.
		stopped := make(chan struct{})
		go func() {
			grpcServer.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-shutdown.Done():
			grpcServer.Stop()
		}
		return server.Shutdown(shutdown)
	}
}
