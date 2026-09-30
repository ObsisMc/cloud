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
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/wanglongan587/cloud/internal/api/router"
	"github.com/wanglongan587/cloud/internal/collab"
	"github.com/wanglongan587/cloud/internal/config"
	"github.com/wanglongan587/cloud/internal/controlgrpc"
	"github.com/wanglongan587/cloud/internal/core"
	"github.com/wanglongan587/cloud/internal/logger"
	"github.com/wanglongan587/cloud/internal/pluginmarket"
	"github.com/wanglongan587/cloud/internal/repository"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

// configureCollaboration installs the optional development Agent/Team/Workflow fixtures on the store
// when the deployment explicitly enables them (`collaboration.development_fixtures`). It is the
// cmd/server composition gate: production default is OFF, leaving the collaboration ports nil so the
// target discovery API serves only human targets. It is intentionally independent of authentication —
// enabling GitHub Auth must never enable these fixtures, and an auth failure must never fall back to
// a fixture identity.
func configureCollaboration(store *core.Store, developmentFixtures bool, log *zap.Logger) {
	if developmentFixtures {
		collab.WireDevelopmentFixtures(store)
		log.Warn("development collaboration fixtures enabled: Agent/Team/Workflow targets served from in-memory fixtures (development-only; production must leave collaboration.development_fixtures false)")
		return
	}
	// Production: Agent is the one collaboration target with real backing (space_agents). Wire a
	// roster-backed directory so target discovery serves real Space Agents; Team/Workflow (cross-module
	// ports, still unbuilt) stay unreported. Run-create resolution re-validates against space_agents
	// in-transaction regardless of this directory.
	if store.Pool != nil {
		store.Directory = core.NewSpaceAgentDirectory(store.Pool)
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
	configureCollaboration(store, cfg.Collaboration.DevelopmentFixtures, log)
	// Production A-side execution seam: replace the fail-closed Unavailable control plane with the
	// real StoreAgentRunControlPlane so EnqueueExecutionWork persists execution_work in the caller's
	// transaction (G-001 execution portion, G-008). The workspace/thread seams still fail closed at
	// this slice, so G-001 stays PARTIAL; only the execution seam is wired real here.
	store.AgentRunControlPlane = core.NewStoreAgentRunControlPlane()
	if e := store.CheckSchema(ctx); e != nil {
		return e
	}
	auth, e := core.NewAuthenticator(cfg.Auth.Audience, cfg.Auth.Keys)
	if e != nil {
		return e
	}
	var directory router.Directory
	if cfg.Directory.Endpoint != "" {
		key, readErr := os.ReadFile(cfg.Directory.AppKeyFile)
		if readErr != nil {
			return fmt.Errorf("read directory app key: %w", readErr)
		}
		directory, e = router.NewTianzhouClient(cfg.Directory.Endpoint, cfg.Directory.HWID, cfg.Directory.Environment, string(key), nil)
		if e != nil {
			return e
		}
	}
	// The marketplace sync loop is owned by this process like gateway.RunCleanup:
	// the ctx cancellation on shutdown stops it and the WaitGroup below waits
	// for the in-flight sync (network + scan only; no database transaction
	// spans them) before the process exits.
	var syncGroup sync.WaitGroup
	if cfg.Plugins.SyncEnabled {
		source := pluginmarket.Source{Namespace: "official", URL: cfg.Plugins.MarketplaceURL, Branch: cfg.Plugins.MarketplaceBranch}
		syncer := pluginmarket.NewSyncer(source, filepath.Join(os.TempDir(), "ora-cloud-plugins", "official"), store, log)
		syncGroup.Add(1)
		go func() {
			defer syncGroup.Done()
			pluginmarket.RunSyncLoop(ctx, syncer.Sync, cfg.Plugins.SyncInterval, pluginmarket.ContextSleep, log)
		}()
	}
	// The agent-run dispatch loop is B-owned recovery for queued real Space Agent runs: a bounded
	// rescan (limit agentDispatchBatchSize, ordered by created_at,id) that moves a queued run to
	// phase='provisioning', status='dispatched' only when the AgentRunControlPlane accepts, and leaves
	// busy or failed runs queued to retry at the next tick. It is owned by the process lifecycle like
	// the marketplace loop: ctx cancellation stops it and the WaitGroup below waits for the in-flight
	// pass (each Dispatch runs in its own short transaction; no transaction spans the sleep). The
	// <=10s cadence means a project that was busy at claim time unblocks within a tick.
	{
		const agentDispatchInterval = 10 * time.Second
		syncGroup.Add(1)
		go func() {
			defer syncGroup.Done()
			pluginmarket.RunSyncLoop(ctx, store.DispatchQueuedAgentRunsOnce, agentDispatchInterval, pluginmarket.ContextSleep, log)
		}()
	}
	// Phase 3A session-start loop: B-owned recovery for runs settled into phase='starting' (their run
	// Workspace is provisioned and admitted). A bounded rescan (limit agentSessionStartBatchSize, ordered
	// by created_at,id, excluding already-started runs and cancelled requests) releases each run's
	// exactly-once first produce: thread_entries seq=1 + one EnqueueExecutionWork item in one transaction
	// (D-014/D-015). The run leaves 'starting' only in Phase 4 on takeover evidence, so this loop stays
	// idempotent alongside the dispatch loop. Cadence is a repository-preferred 10s constant
	// (IMPLEMENTATION CHOICE — plan §3 does not fix the seconds); ctx cancellation stops it and the
	// WaitGroup waits for the in-flight pass.
	{
		const agentSessionStartInterval = 10 * time.Second
		syncGroup.Add(1)
		go func() {
			defer syncGroup.Done()
			pluginmarket.RunSyncLoop(ctx, store.StartQueuedAgentSessionsOnce, agentSessionStartInterval, pluginmarket.ContextSleep, log)
		}()
	}
	gin.SetMode(cfg.Server.Mode)
	server := &http.Server{Addr: fmt.Sprintf(":%d", cfg.Server.Port), Handler: router.New(store, auth, log, directory), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: cfg.Server.ReadTimeout, WriteTimeout: cfg.Server.WriteTimeout, IdleTimeout: 60 * time.Second}
	// The control listener is bound before serving so a taken port fails startup, not a Controller.
	control, e := net.Listen("tcp", cfg.Control.GRPCAddr)
	if e != nil {
		return e
	}
	grpcServer := controlgrpc.New(store)
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
		cancel()
		syncGroup.Wait()
		if errors.Is(e, http.ErrServerClosed) || errors.Is(e, grpc.ErrServerStopped) {
			return nil
		}
		return e
	case <-ctx.Done():
		shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		// In-flight control calls finish or are cut at the same deadline as HTTP; a Controller
		// retries with the same submission identity, so cutting them loses nothing durable.
		// Tell the lease holder to stop claiming before its stream is cut; the Drain signal is a
		// hint, so a Controller that misses it simply fails its next claim against a stopped server.
		store.Signals.Drain()
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
		e = server.Shutdown(shutdown)
		syncGroup.Wait()
		return e
	}
}
