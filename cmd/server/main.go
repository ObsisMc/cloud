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
	"github.com/wanglongan587/cloud/internal/objectstore"
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
	// Production B-side transition seam: bind the real hook set so a control-plane transaction that
	// persists Node evidence drives the matching IssueRun/Thread transition in the same transaction
	// — the Phase 4B Thread takeover (starting→running, thread_entries, thread_state) and the Phase
	// 2A workspace settlement. Without it, Store.Control falls back to UnavailableAgentRunHooks and
	// every takeover would roll back fail-closed.
	store.AgentRunHooks = core.NewBusinessAgentRunHooks(store)
	// Cloud Revision D1: object storage is Cloud's own capability, and it is optional. A deployment
	// that leaves `object_store` out still runs — every upload grant is then refused as UNAVAILABLE,
	// the delivery fails deterministically, and IssueRun D5's give-up window closes the run out — so
	// the missing capability is reported here and never substituted for a working one.
	//
	// A section that IS configured must produce a client: a malformed endpoint, a bad bucket name or
	// unreadable credential files fail startup before any run is left waiting on a capability this
	// process can never provide. An endpoint that is merely unreachable, or a bucket that does not
	// exist yet, is not a startup error (D1) — it surfaces as a failed object verification, which D5
	// already retries.
	if cfg.ObjectStore.Configured() {
		objects, e := objectstore.New(objectstore.Config{
			Endpoint:            cfg.ObjectStore.Endpoint,
			PublicEndpoint:      cfg.ObjectStore.PublicEndpoint,
			Region:              cfg.ObjectStore.Region,
			Bucket:              cfg.ObjectStore.Bucket,
			PathStyle:           cfg.ObjectStore.PathStyle,
			AccessKeyIDFile:     cfg.ObjectStore.AccessKeyIDFile,
			SecretAccessKeyFile: cfg.ObjectStore.SecretAccessKeyFile,
		})
		if e != nil {
			return e
		}
		store.RevisionObjects = objects
		store.RevisionUploadTTL = cfg.ObjectStore.UploadGrantTTL
	} else {
		log.Warn("object_store is not configured: Revision upload grants will be refused and every delivery will fail until IssueRun D5 gives up")
	}
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
	// Phase 4C Thread-ending loops: B-owned recovery for the two ending triggers that are reachable
	// without a new public API. The idle scan ends a Thread whose idle window has expired
	// (thread_state='idle' with idle_since older than cfg.IssueRuns.ThreadIdleTimeout), and the
	// cancel pass reacts to a cancellation request recorded on a live run. Each run is handled in
	// its own short transaction and each transition is a CAS, so a repeated tick emits no second
	// EndSession. The cadence is the same repository-preferred 10s as the loops above and must stay
	// far below the idle window (an implementation choice; plan §3 fixes no seconds, D-008 is the
	// precedent). Both stop at thread_state='ending': the session terminal (`ended`) and everything
	// after it belong to Phase 5.
	{
		const agentThreadEndInterval = 10 * time.Second
		store.ThreadIdleTimeout = cfg.IssueRuns.ThreadIdleTimeout
		syncGroup.Add(1)
		go func() {
			defer syncGroup.Done()
			pluginmarket.RunSyncLoop(ctx, store.EndIdleAgentThreadsOnce, agentThreadEndInterval, pluginmarket.ContextSleep, log)
		}()
		syncGroup.Add(1)
		go func() {
			defer syncGroup.Done()
			pluginmarket.RunSyncLoop(ctx, store.ReactToCancelledAgentRunsOnce, agentThreadEndInterval, pluginmarket.ContextSleep, log)
		}()
	}
	// Phase 5 Batch 2 delivery/release loops. The first applies IssueRun D5's give-up limits to runs
	// still `delivering`: once delivery has failed for cfg.IssueRuns.DeliveryGiveUpAfter, or the run
	// Workspace's Node has been unreachable for cfg.IssueRuns.DeliveryUnreachableAfter, the run is
	// released with deliveryState=failed and its Workspace delete declared. The second reconciles the
	// release half: a `releasing` run whose delete_workspace operation has no operation in flight gets
	// its delete re-declared, which is the Cloud-side retry operation D4 requires after a Node refuses
	// to quiesce (the run Workspace has no public API and no user to retry it). The third applies
	// IssueRun D8's single give-up limit to the delete side: a `releasing` run whose Workspace Node has
	// been unknown for cfg.IssueRuns.DeliveryUnreachableAfter (the same window — D8 adds no new one) is
	// settled `done` with failure_reason=workspace_unavailable, its delete operation left terminally
	// failed and its Workspace row retained; `done` there means Cloud stopped retrying, never that the
	// Workspace was released. All three are bounded passes of one short transaction per run, so a
	// repeated tick produces no second release, no second delete operation, and no second phase
	// transition. Cadence is the same repository-preferred 10s
	// as the loops above (an implementation choice; plan §3 fixes no seconds).
	{
		const agentDeliveryInterval = 10 * time.Second
		store.DeliveryGiveUpAfter = cfg.IssueRuns.DeliveryGiveUpAfter
		store.DeliveryUnreachableAfter = cfg.IssueRuns.DeliveryUnreachableAfter
		syncGroup.Add(1)
		go func() {
			defer syncGroup.Done()
			pluginmarket.RunSyncLoop(ctx, store.GiveUpStaleDeliveriesOnce, agentDeliveryInterval, pluginmarket.ContextSleep, log)
		}()
		syncGroup.Add(1)
		go func() {
			defer syncGroup.Done()
			pluginmarket.RunSyncLoop(ctx, store.RedeclareRunWorkspaceDeletesOnce, agentDeliveryInterval, pluginmarket.ContextSleep, log)
		}()
		syncGroup.Add(1)
		go func() {
			defer syncGroup.Done()
			pluginmarket.RunSyncLoop(ctx, store.GiveUpStaleWorkspaceReleasesOnce, agentDeliveryInterval, pluginmarket.ContextSleep, log)
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
