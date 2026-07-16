//go:build windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"sync"

	"everevo/internal/backends"
	"everevo/internal/backends/llama"
	"everevo/internal/backends/onnx"
	"everevo/internal/config"
	"everevo/internal/core"
	"everevo/internal/downloader"
	"everevo/internal/guides"
	"everevo/internal/httpclient"
	"everevo/internal/memory"
	"everevo/internal/rag"
	"everevo/internal/model"
	"everevo/internal/storage"
	"everevo/internal/sysinfo"
	"everevo/internal/taskboard"
	"everevo/internal/wiki"

	"everevo/internal/a2a"
	"everevo/internal/acp"
	"everevo/internal/agents"
	agentPlugin "everevo/internal/plugins/tools/agents"
	"everevo/internal/async"
	evolvePlugin "everevo/plugins/tools/evolve"
	modelsPlugin "everevo/internal/plugins/tools/models"
	"everevo/internal/collab"
	"everevo/internal/feishu"
	"everevo/internal/mcp"
	mcpclient "everevo/internal/mcp/client"
	"everevo/internal/skills"
	"everevo/internal/tools"
	"everevo/internal/workflow"
	"everevo/internal/zone"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 是 Wails 应用结构体。所有公开方法自动暴露给前端。
type App struct {
	ctx             context.Context
	chatCtx         context.Context
	chatCancel      context.CancelFunc
	cfg             *config.Config
	manager         *model.Manager
	sysInfoCache    *sysinfo.SysInfo
	dlManager       *downloader.Manager
	mcpServer       *mcp.Server
	mcpClient       *mcpclient.Manager
	a2aManager      *a2a.Manager
	feishuClient    *feishu.Client
	skillManager     *skills.Manager
	paradigmManager  *memory.ParadigmManager
	agentManager     *agents.Manager
	memoryStore     *memory.Store
	guideManager    *guides.Manager
	workflowManager *workflow.Manager
	collab          *collab.Kernel
	activityQueue   chan memory.ActivityRow            // unified AI-work log write queue
	memSweepDone    chan struct{}
	wikiStores      map[string]*wiki.Store           // per-library wiki stores, keyed by libraryID
	wikiStoreMu     sync.RWMutex
	activeStreams   int32                                // atomic counter; >0 → skip extraction
	streamCancelMu  sync.Mutex
	streamCancels   map[string]context.CancelFunc    // streamID → cancel
	zone            *zone.Zone                        // current runtime zone
	sourceDir       string                             // project root for self-compilation ("" if unavailable)
	acpBridge       *acp.Bridge                        // OpenCode ACP bridge for code modification tasks
	taskBoard       *taskboard.Board
	asyncManager    *async.Manager
	commandQueue    *async.CommandQueue    // unified async result notification queue
	agentTaskState  *async.AgentTaskState  // in-memory registry of running agent tasks
	fileCtl         FileCtl                             // file access control (readonly/audit/full)
}

// NewApp 创建应用实例。
func New() *App {
	return &App{}
}

// initWindowSize sizes and centers the window at 72% × 78% of the primary
// screen's logical dimensions. Respects MinWidth/MinHeight and never exceeds
// the physical screen bounds.
func initWindowSize(ctx context.Context) {
	screens, err := wailsRuntime.ScreenGetAll(ctx)
	if err != nil || len(screens) == 0 {
		return
	}

	// Use first screen (primary on single-monitor, usually primary on multi).
	s := screens[0]

	const (
		widthRatio  = 0.72
		heightRatio = 0.78
	)

	w := int(float64(s.Size.Width) * widthRatio)
	h := int(float64(s.Size.Height) * heightRatio)

	// Clamp to min/max bounds.
	if w < 640 {
		w = 640
	}
	if h < 480 {
		h = 480
	}
	if w > s.Size.Width {
		w = s.Size.Width
	}
	if h > s.Size.Height {
		h = s.Size.Height
	}

	wailsRuntime.WindowSetSize(ctx, w, h)
	wailsRuntime.WindowCenter(ctx)
}

// startup 在 Wails 应用启动时调用。
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.chatCtx, a.chatCancel = context.WithCancel(context.Background())

	// ── Adaptive window sizing ─────────────────────────────────
	// Default size is 72% × 78% of the primary screen's work area,
	// constrained by MinWidth/MinHeight and physical screen bounds.
	initWindowSize(ctx)

	// 日志同时写到文件和终端（dev 模式下终端可见，生产 EXE 只看文件）
	// Data dirs and legacy migration are handled below with error logging.

	// Ensure this zone exists and allocate ports.
	a.initZone()

	dataDir, _ := storage.AppDataDir()
	logPath := filepath.Join(dataDir, "EverEvo.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	var logW io.Writer = os.Stdout
	if err == nil {
		logW = io.MultiWriter(os.Stdout, logFile)
	}
	log.SetOutput(logW)
	log.SetFlags(log.Ltime | log.Lmsgprefix)
	log.SetPrefix("[EverEvo] ")
	log.Printf("日志文件: %s", logPath)
	log.Println("══════════━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("应用启动")

	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("应用启动中……")

	cfg, err := config.Load()
	if err != nil {
		log.Printf("⚠ 加载用户配置失败: %v，使用默认配置", err)
		cfg = config.Defaults()
	}
	a.cfg = cfg
	log.Printf("用户配置: %s", config.Path())
	// Apply proxy config
	httpclient.SetUserProxy(cfg.LLM.HTTPProxy)
	if cfg.LLM.ProxyEnabled != nil && !*cfg.LLM.ProxyEnabled {
		httpclient.SetEnabled(false)
		log.Printf("ℹ 代理已禁用（用户设置）")
	}
	if ps := httpclient.Detect(); ps.Source != "none" {
		log.Printf("✓ 网络代理: %s (来源: %s)", ps.URL, ps.Source)
	}
	// Async proxy health check (logs warning if unreachable)
	go httpclient.HealthCheck()

	log.Printf("项目根目录: %s", storage.ProjectRoot())

	if err := storage.EnsureDirs(); err != nil {
		log.Printf("创建数据目录失败: %v", err)
	}
	log.Printf("数据目录: %s", storage.DataDir())
	log.Printf("模型目录: %s", storage.ModelsDir())

	// 尝试初始化 ONNX Runtime（如果已安装）
	onnxDLL, _ := findONNXDLL()
	if onnxDLL != "" {
		if err := onnx.Init(onnxDLL); err != nil {
			log.Printf("⚠ ONNX Runtime 初始化失败: %v (DLL: %s)", err, onnxDLL)
		} else {
			log.Printf("✓ ONNX Runtime 就绪 (%s)", filepath.Base(onnxDLL))
		}
	} else {
		log.Println("ℹ 未找到 ONNX Runtime DLL，推理功能不可用")
	}

	// 尝试初始化 llama.cpp（best-effort，通过 llama-server.exe 子进程）
	llamaBin, _ := findLlamaDLL()
	if llamaBin != "" {
		if err := llama.Init(llamaBin); err != nil {
			log.Printf("⚠ llama.cpp 初始化失败: %v", err)
		} else {
			log.Printf("✓ llama.cpp 就绪 (%s)", filepath.Base(llamaBin))
		}
	} else {
		log.Println("ℹ 未找到 llama-server.exe，GGUF 模型将无法推理")
		log.Println("   下载: https://github.com/ggml-org/llama.cpp/releases")
	}

	a.manager = model.NewManager()
	a.dlManager = downloader.NewManager(func(event string, data interface{}) {
		wailsRuntime.EventsEmit(a.ctx, event, data)
	})
	// Persist download history across restarts.
	if dataDir, err := storage.AppDataDir(); err == nil {
		historyPath := filepath.Join(dataDir, "download_history.json")
		a.dlManager.SetHistoryPath(historyPath)
		a.dlManager.LoadHistory()
		log.Printf("下载历史: %s (%d 条记录)", historyPath, len(a.dlManager.History()))
	}
	log.Println("模型管理器就绪")

	// Wire models plugin with App infrastructure (catalog/download logic).
	modelsPlugin.SetBackend(a)
	log.Println("模型插件后端已接入")

	// Register all LLM-callable tools
	tools.RegisterAll()
	log.Printf("已注册 %d 个 LLM 工具", len(tools.List()))

	// Build plugin tool dispatch map so CallTool can route migrated tools
	// through core.ToolPlugin instances (model/catalog/download/provider/plugin ops).
	BuildPluginToolMap()

	// Initialize skill manager
	a.skillManager = skills.NewManager()
	if err := a.skillManager.Save(); err != nil {
		log.Printf("[skills] 初次持久化失败: %v", err)
	}
	log.Printf("已加载 %d 个能力域 (Skill)", len(a.skillManager.List()))

	// Initialize local agent manager (personas) — ensures a default main agent exists
	a.agentManager = agents.NewManager()
		a.paradigmManager = memory.NewParadigmManager()
		log.Printf("已加载 %d 个思维范式", len(a.paradigmManager.List()))

	log.Printf("已加载 %d 个本地 Agent", len(a.agentManager.List()))


	// Wire agent execution plugin IMMEDIATELY after agent manager init.
	// Must be before memory store and any conditional blocks — LLM tool
	// calls (agent_run, agent_list) can arrive once the Wails frontend connects.
	a.commandQueue = async.NewCommandQueue()
	a.agentTaskState = async.NewAgentTaskState()
	agentPlugin.SetDeps(&agentPlugin.Deps{
		Cfg:          a.cfg,
		SkillManager: a.skillManager,
		AgentManager: a.agentManager,
		MemoryStore:  a.memoryStore,
		MCPClient:    a.mcpClient,
		ChatCompletion: func(p *config.LLMProvider, messagesJSON, toolsJSON json.RawMessage, opts agentPlugin.ChatOpts) (map[string]any, error) {
			return a.chatCompletion(p, messagesJSON, toolsJSON, chatOpts{
				Temperature: opts.Temperature,
				MaxTokens:   opts.MaxTokens,
				ThinkEffort: opts.ThinkEffort,
				OnChunk:     opts.OnChunk,
				Ctx:         opts.Ctx,
			})
		},
		CallTool: a.CallTool,
		Collab:   a.collab,
		CommandQueue:   a.commandQueue,
		AgentTaskState: a.agentTaskState,
		TaskManager:    a.asyncManager,
		EnrichSystemPrompt: func(base, userQuery, libraryID string) string {
			return a.enrichAgentPrompt(base, userQuery, libraryID)
		},
	})
	log.Println("Agent 执行插件已就绪")

	// Initialize collaboration kernel (event bus, blackboard, dispatcher).
	// Forwards backend collab events to the Wails frontend for visualization,
	// AND records every event into the unified activity log (single chokepoint).
	// The dispatcher is created local-only here; remote delivery is wired
	// after the A2A manager starts (see below).
	a.activityQueue = make(chan memory.ActivityRow, activityQueueCap) // allocated early so collab.ready etc. are captured
	a.collab = collab.NewKernel(func(topic string, data any) {
		if ev, ok := data.(collab.Event); ok {
			a.recordActivity(topic, ev)
		}
		if a.ctx != nil {
			wailsRuntime.EventsEmit(a.ctx, "collab:event", map[string]any{"topic": topic, "data": data})
		}
	})
	a.collab.SetDispatcher(collab.NewDispatcher(a.collab, agentRunnerAdapter{a: a}, nil))
	for _, ag := range a.agentManager.List() {
		a.collab.Dispatch.RegisterLocal(ag.ID)
	}
	log.Println("协同内核就绪 (event bus + blackboard + dispatcher)")
	// Handshake: notify the frontend workbench that the kernel is live, so its
	// "连接中" badge can flip to "已连接·空闲" even with zero collaboration activity.
	a.collab.Bus.Publish("collab.ready", collab.Event{Type: "ready"})

	// Initialize MCP Client — auto-connect external MCP servers
	a.mcpClient = mcpclient.NewManager()
	a.mcpClient.LoadAndConnect()

	// Initialize A2A Agent Manager — server + client for agent-to-agent communication
	a.initA2AManager()
	log.Println("A2A Agent 管理器就绪")

	// Wire remote agent delivery into the collaboration dispatcher now that
	// the A2A manager (and its remote-agent client connections) are ready.
	if a.collab != nil && a.collab.Dispatch != nil {
		a.collab.Dispatch.SetRemote(a2aRemoteAdapter{a: a})
	}

	// Initialize Feishu bot (WebSocket long-connection to Feishu)
	a.initFeishuClient()
	log.Println("飞书机器人就绪")

	// Initialize guide manager. On first run it seeds the bundled EverEvo usage
	// guides (local source); trigger an initial sync so the Guide Center is
	// populated immediately rather than showing "0 来源 0 文档".
	var guidesSeeded bool
	a.guideManager, guidesSeeded = guides.NewManager()
	log.Printf("已加载 %d 个攻略来源", len(a.guideManager.ListSources()))
	if guidesSeeded {
		go a.guideManager.SyncAll()
	}

	// Initialize workflow manager
	a.workflowManager = workflow.NewManager()
	log.Printf("已加载 %d 个工作流", len(a.workflowManager.List()))

	// Initialize memory store (conversation persistence; the temporal knowledge
	// graph arrives in P2 using the same SQLite handle).
	a.memoryStore, err = memory.NewStore()
	if err != nil {
		log.Printf("⚠ 记忆数据库初始化失败: %v", err)
	} else {
		log.Println("记忆数据库就绪")
		// P1: bind an embedding model for long-term semantic memory. Auto-detect
		// the first sentence-embedding model under data/models; degrade silently
		// (recall stays empty) if none is installed.
		if a.memoryStore.EmbeddingModelDir() == "" {
			if dir := detectEmbeddingModelDir(); dir != "" {
				if e := a.memoryStore.SetEmbeddingModel(dir); e == nil {
					log.Printf("记忆向量模型: %s", filepath.Base(dir))
				}
			} else {
				log.Println("ℹ 未找到句向量模型，长期记忆检索将关闭（下载 sentence-transformers 模型即可启用）")
			}
		}
	}

		// Wire paradigm embedder for semantic matching in Recommend().
		if a.paradigmManager != nil {
			if dir := a.memoryStore.EmbeddingModelDir(); dir != "" {
				a.paradigmManager.SetEmbedder(func(text string) ([]float32, error) {
					return rag.EmbedQuery(dir, text)
				})
				if err := a.paradigmManager.BuildEmbeddings(); err != nil {
					log.Printf("[paradigm] 嵌入构建失败: %v", err)
				}
			}
		}

		// Register memory as a core.MemoryPlugin so the Engine can discover it.
		mp := memory.NewMemoryPlugin(a.memoryStore, func(ctx context.Context, text string) ([]float32, error) {
			dir := a.memoryStore.EmbeddingModelDir()
			if dir == "" {
				return nil, fmt.Errorf("no embedding model configured")
			}
			return rag.EmbedQuery(dir, text)
		})
		core.GlobalMemories.Register("memory", mp, mp.Manifest())

	// P5: compute hardware-adaptive memory policy (RAM/disk → tier → params).
	a.applyMemoryPolicy()

	// Wire blackboard persistence to SQLite.
	a.collab.SetBlackboardPersistFn(func(boardID, key, value, author, kind string, updatedAt time.Time) {
		if a.memoryStore == nil {
			return
		}
		if value == "" && kind == "" {
			_ = a.memoryStore.BBDeleteEntry(boardID, key)
		} else {
			_ = a.memoryStore.BBSaveEntry(boardID, key, value, author, kind, updatedAt.UnixMilli())
		}
	})
	a.collab.SetBlackboardLoadFn(func(boardID string) []collab.Entry {
		if a.memoryStore == nil {
			return nil
		}
		rows, err := a.memoryStore.BBLoadEntries(boardID)
		if err != nil {
			return nil
		}
		out := make([]collab.Entry, 0, len(rows))
		for _, r := range rows {
			out = append(out, collab.Entry{
				Key: r.Key, Value: r.Value, Author: r.Author,
				Kind: r.Kind, UpdatedAt: time.UnixMilli(r.UpdatedAt),
			})
		}
		return out
	})

	// Restore active collaboration sessions now that the memory store is ready.
	// (Must run AFTER memoryStore init — earlier the guard was always nil.)
	if a.collab != nil && a.memoryStore != nil {
		if rows, err := a.memoryStore.ListCollabSessions(); err == nil {
			for _, r := range rows {
				if r.Status != collab.SessionActive {
					continue
				}
				var members []collab.Member
				for _, m := range r.Members {
					members = append(members, collab.Member{AgentID: m.AgentID, Role: m.Role})
				}
				a.collab.Sessions.Restore(r.ID, r.Goal, r.OrchestratorID, r.BlackboardID, r.Status, members, time.UnixMilli(r.CreatedAt))
			}
			if len(rows) > 0 {
				log.Printf("[collab] 恢复 %d 个协同会话", len(rows))
			}
		}
		// Restore active plans from the JSON snapshot.
		if a.collab != nil {
			if dir, dErr := storage.AppDataDir(); dErr == nil {
				if n, err := a.collab.Plans.RestoreFrom(filepath.Join(dir, "plans.json")); err == nil && n > 0 {
					log.Printf("[collab] 恢复 %d 个计划", n)
				}
			}
		}
	}


	// Initialize OpenCode ACP bridge for code modification delegation.
	a.acpBridge = acp.NewBridge(acp.DefaultExe)
	log.Println("ACP 桥接就绪 (OpenCode)")

	// Wire the evolve plugin (build, swap, ACP, sandbox).
	evolvePlugin.New(evolveDelegate{a})
	log.Println("进化插件已就绪")

	// Auto-resume ACP evolve tasks that were interrupted mid-flight.
	a.ResumeAcpEvolveTasks()

	// Resume pending evolve tasks (e.g. a swap that was interrupted mid-restart).
	// 同时检测重启标记：区分「自进化重启」和「普通崩溃重启」。
	if marker := a.readRestartMarker(); marker != nil {
		log.Printf("[evolve] 🔄 检测到自进化重启 (reason=%s, task=%s, time=%s)", marker.Reason, marker.TaskID, marker.Timestamp)
	} else {
		log.Println("[evolve] ℹ 未检测到重启标记（普通启动或首次运行）")
	}
	if pending := a.ResumeEvolveTasks(); len(pending) > 0 {
		log.Printf("[evolve] ⚠ %d 个任务需要跟进（详见 wiki 的 evolve/next_action）", len(pending))
	}

	// P5: boot + daily TTL sweep of expired episodic memory (core layer untouched).
	a.memSweepDone = make(chan struct{})
	go a.runMemorySweep()
	// Unified activity log writer: drains activityQueue (fed by the collab bus
	// forward callback + workflow bridge) into SQLite, off the event bus.
	go a.runActivityWriter()

	// P7: seed default workspace + domain library
	if a.memoryStore != nil {
		_, _ = a.memoryStore.DefaultWorkspace()
		libID, _ := a.memoryStore.DefaultLibrary()
		// Collect valid domain library IDs for dangling-reference repair.
		validIDs := a.memoryStore.ListLibraryIDs()

		// Backfill legacy agents with the default library ID (fixes empty + dangling).
		if libID != "" && a.agentManager != nil {
			_ = a.agentManager.EnsureLibraryIDs(libID, validIDs)
		}
		// P10: Backfill MCP servers + Skills + KBs with default library ID.
		if libID != "" {
			if a.mcpClient != nil {
				a.mcpClient.BackfillLibraryIDs(libID, validIDs)
					// Heuristic reassignment: move MCP servers from default
					// to matching domains (e.g. SearXNG→搜索, GitHub→开发).
					if libs, err := a.memoryStore.LibraryList(); err == nil {
						nameToID := make(map[string]string)
						for _, l := range libs {
							nameToID[l.Name] = l.ID
						}
						a.mcpClient.ReassignByHeuristic(libID, nameToID)
					}
			}
			if a.skillManager != nil {
				_ = a.skillManager.EnsureLibraryIDs(libID, validIDs)
			}
			// Backfill KB library IDs (fixes empty + dangling, e.g. orphan lib_18c0842c0b947f70).
			if ragStore, err := a.getRagStore(); err == nil {
				ragStore.BackfillLibraryIDs(libID, validIDs)
				// Wire bidirectional chunk registry: after RAG docs are added,
				// register chunk→source mappings for hierarchical retrieval.
				if a.memoryStore != nil {
					ragStore.SetChunkRegistrar(func(sourceType, sourceID string, docIDs []string, chunkStartIndex int, contents []string) error {
						entries := make([]memory.ChunkRegistryEntry, len(docIDs))
						now := time.Now().UnixMilli()
						for i, id := range docIDs {
							entries[i] = memory.ChunkRegistryEntry{
								ChunkID:    id,
								SourceType: sourceType,
								SourceID:   sourceID,
								ChunkIndex: chunkStartIndex + i,
								ChunkType:  "leaf",
								CreatedAt:  now,
							}
						}
						return a.memoryStore.RegisterChunks(entries)
					})
				}
			}
			// Migrate historical graph nodes from "default" to their proper domains.
			if migrated, mErr := a.MemoryGraphMigrate(); mErr == nil && migrated > 0 {
				log.Printf("[memory] 启动图谱迁移: %d 节点分配到所属领域", migrated)
			}
			// One-time historical fact dedup (memory_items.fact + user_facts).
			if a.memoryStore != nil {
				_ = a.memoryStore.DedupAllFacts()
				if removed, err := a.memoryStore.DedupUserFacts(); err == nil && removed > 0 {
					log.Printf("[memory] 核心记忆(user_facts)去重完成: 移除 %d 条", removed)
				}
				// Prune auto-created libraries that never received any data.
				a.memoryStore.PruneEmptyAutoLibraries()
			}
		}
		// Migrate legacy 'default' workspace_id to actual library ID.
		if libID != "" && a.memoryStore != nil {
			a.memoryStore.MigrateDefaultWorkspace(libID)
		}
	}

	// Async task manager (shares memory.db for persistence).
	if a.memoryStore != nil {
		if mgr, aErr := async.NewManager(a.memoryStore.DB(), func(event string, data any) {
			wailsRuntime.EventsEmit(a.ctx, event, data)
		}); aErr == nil {
			a.asyncManager = mgr
			log.Println("异步任务管理器就绪")
		}
	}

	// P6.1: wiki index — per-library, lazy-init. Core library indexes docs/ at boot.
	a.wikiStores = make(map[string]*wiki.Store)
	if coreLibID, _ := a.memoryStore.DefaultLibrary(); coreLibID != "" {
		if ws, wErr := wiki.NewStore(coreLibID); wErr == nil {
			a.wikiStores[coreLibID] = ws
			go func() { _, _ = a.WikiReindex(coreLibID) }()
		} else {
			log.Printf("⚠ wiki 索引初始化失败: %v", wErr)
		}
	}
	// P9: start dream pipeline scheduler (Light→REM→Deep).
	a.startDreamScheduler()

	// Task board: cross-conversation progress tracking. Loads from JSON,
	// falls back to wiki backup, creates empty if neither exists.
	a.loadTaskBoard()

	// Start the internal MCP server. Auto-start with a default port when no
	// port is configured (previously stayed off — mcp_status showed port:0 and
	// external MCP clients couldn't reach EverEvo). Persist the chosen port.
	if a.cfg.LLM.MCPPort <= 0 {
		a.cfg.LLM.MCPPort = 19400
		config.Save(a.cfg)
	}
	a.StartMCPServer()
}

// ─── Domain validation helpers ──────────────────────────────────────

// validateLibraryID checks that the given library ID is non-empty and points to
// an existing domain_libraries row. Used by all create-entity APIs to enforce
// domain-as-container invariants.
func (a *App) validateLibraryID(libraryID string) error {
	if libraryID == "" {
		return fmt.Errorf("领域 ID 不能为空")
	}
	if a.memoryStore != nil && !a.memoryStore.IsValidLibrary(libraryID) {
		return fmt.Errorf("领域 %q 不存在", libraryID)
	}
	return nil
}

// resolveLibraryID returns the given library ID if non-empty; otherwise returns
// the default library ID. Used by list/query APIs for backward compatibility.
func (a *App) resolveLibraryID(libraryID string) string {
	if libraryID != "" {
		return libraryID
	}
	if a.memoryStore != nil {
		if id, err := a.memoryStore.DefaultLibrary(); err == nil {
			return id
		}
	}
	return ""
}

// ─── Startup helpers ──────────────────────────────────────────────

// findONNXDLL searches for onnxruntime.dll using the unified backends scanner.
func findONNXDLL() (string, bool) {
	dll := backends.FindDLL("onnxruntime*.dll")
	return dll, dll != ""
}

// findLlamaDLL searches for llama-server.exe using the unified backends scanner.
func findLlamaDLL() (string, bool) {
	exe := backends.FindDLL("llama-server.exe")
	return exe, exe != ""
}

// detectEmbeddingModelDir returns the path of the first detected
// sentence-embedding model under the models directory.
func detectEmbeddingModelDir() string {
	// Prefer multilingual → handles both Chinese and English content.
	candidates := []string{
		"sentence-transformers_paraphrase-multilingual-MiniLM-L12-v2", // multilingual: zh+en+...
		"sentence-transformers_all-MiniLM-L6-v2",                       // English only (fallback)
		"sentence-transformers_all-mpnet-base-v2",                      // English only (fallback)
		"bge-small-zh-v1.5",                                            // BGE Chinese
		"bge-base-zh-v1.5",                                             // BGE Chinese
	}
	// Search EXE dir + working dir for models.
	exe, _ := os.Executable()
	dirs := []string{filepath.Join(filepath.Dir(exe), "data", "models")}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(wd, "data", "models"))
	}
	dirs = append(dirs, storage.ModelsDir())
	for _, modelsDir := range dirs {
		for _, c := range candidates {
			p := filepath.Join(modelsDir, c)
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				return p
			}
		}
	}
	return ""
}

// ─── Knowledge Graph — P2 view (frontend graph viewer) ───────────

// initZone ensures the current runtime zone exists on disk with an up-to-date
// manifest. It allocates ports and creates the zone directory if this is the
// first launch after upgrading.
func (a *App) initZone() {
	zoneName := os.Getenv("EVEREVO_ZONE")
	if zoneName == "" {
		zoneName = "production"
	}

	z, err := zone.Get(zoneName)
	if err != nil {
		// First launch — create the production zone from scratch.
		log.Printf("[zone] 首次启动，创建 %q 运行区", zoneName)
		isProd := zoneName == "production"
		zoneType := zone.TypeExperiment
		if isProd {
			zoneType = zone.TypeProduction
		}

		// For production, create an empty zone.
		dir := zone.Dir(zoneName)
		if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
			log.Printf("[zone] 创建 zone 目录失败: %v", mkErr)
			return
		}

		// Allocate ports.
		mcpPort, a2aPort, pErr := zone.Allocate(zoneName, isProd)
		if pErr != nil {
			log.Printf("[zone] 端口分配失败: %v", pErr)
			return
		}

		m := zone.Manifest{
			Name:      zoneName,
			Type:      zoneType,
			CreatedAt: time.Now(),
			MCPPort:   mcpPort,
			A2APort:   a2aPort,
		}
		_ = zone.WriteManifest(dir, &m)
		z = &zone.Zone{
			Name:      zoneName,
			Dir:       dir,
			Type:      zoneType,
			MCPPort:   mcpPort,
			A2APort:   a2aPort,
			CreatedAt: time.Now(),
		}
	}

	a.zone = z
	log.Printf("[zone] 运行区: %s (type=%s, mcp=%d, a2a=%d)", z.Name, z.Type, z.MCPPort, z.A2APort)

	// Detect source code directory for self-evolution (rebuild from source).
	a.detectSourceDir()
}

// detectSourceDir finds the project root (containing go.mod + main.go) for
// self-compilation. Walks up from the EXE directory through ancestor chains,
// then falls back to the working directory.
//
// Typical layouts that Just Work:
//
//	EXE at build/bin/everevo.exe     → source at ../..  (wails build)
//	EXE at project root/everevo.exe  → source at .      (user copies EXE to root)
//	EXE anywhere, wd = project root  → source at wd     (wails dev)
func (a *App) detectSourceDir() {
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)

	// Strategy 1: walk UP from EXE directory looking for go.mod + main.go.
	// This covers the common case: build/bin/everevo.exe → project root.
	var candidates []string
	for d := exeDir; d != "" && d != filepath.Dir(d); d = filepath.Dir(d) {
		candidates = append(candidates, d)
		// Don't walk past 5 levels — if we haven't found it by then, it's not here.
		if len(candidates) >= 5 {
			break
		}
	}

	// Strategy 2: working directory (wails dev mode).
	if wd, err := os.Getwd(); err == nil {
		absWd, _ := filepath.Abs(wd)
		candidates = append(candidates, absWd)
	}

	// Strategy 3: common relative paths from EXE dir (belt-and-suspenders).
	for _, rel := range []string{"..", "..\\..", "..\\..\\.."} {
		candidates = append(candidates, filepath.Join(exeDir, rel))
	}

	// Deduplicate while preserving order.
	seen := map[string]bool{}
	var unique []string
	for _, d := range candidates {
		abs, err := filepath.Abs(d)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		if !seen[abs] {
			seen[abs] = true
			unique = append(unique, abs)
		}
	}

	// Check each candidate for go.mod (with module "everevo") + main.go.
	for _, d := range unique {
		if isEverEvoRoot(d) {
			a.sourceDir = d
			exeRel := ""
			if rel, err := filepath.Rel(d, exeDir); err == nil {
				exeRel = rel
			}
			log.Printf("[evolve] ✓ 源码目录: %s (EXE 相对路径: %s\\)", d, exeRel)
			log.Printf("[evolve]   构建产物: %s", filepath.Join(d, "dist", "bin", "everevo.exe"))
			return
		}
	}

	log.Println("[evolve] ℹ 未找到源码目录（go.mod + main.go），源码级自进化不可用")
	log.Println("[evolve]   将 EXE 放到项目根目录或在项目根目录运行即可启用")
}

// isEverEvoRoot checks whether a directory is the EverEvo project root.
func isEverEvoRoot(dir string) bool {
	gomod := filepath.Join(dir, "go.mod")
	mainGo := filepath.Join(dir, "main.go")
	if _, e1 := os.Stat(gomod); e1 != nil {
		return false
	}
	if _, e2 := os.Stat(mainGo); e2 != nil {
		return false
	}
	// Quick sanity: go.mod should mention "everevo".
	data, err := os.ReadFile(gomod)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "module everevo")
}

// shutdown is called by Wails before the app exits.
func (a *App) Shutdown(ctx context.Context) {
	log.Println("应用关闭中……")
	// Clean up zone: clear PID entry in manifest (ports are released on
	// next startup's port scan or when explicitly stopped via Launch/Stop).
	if a.zone != nil {
		if m, err := zone.ReadManifest(a.zone.Dir); err == nil {
			m.PID = 0
			_ = zone.WriteManifest(a.zone.Dir, m)
		}
	}
	// Kill internally-managed llama-server subprocesses (model_load path).
		// External llama-server / Ollama are managed by the user.
		a.manager.Shutdown()
		if a.chatCancel != nil {
			a.chatCancel()
		}
	if a.mcpServer != nil {
		if err := a.mcpServer.Stop(); err != nil {
			log.Printf("⚠ EverEvo MCP 服务停止失败: %v", err)
		} else {
			log.Println("EverEvo MCP 服务已停止")
		}
	}
	if a.a2aManager != nil {
		_ = a.a2aManager.StopServer()
		log.Println("A2A Agent 服务已停止")
	}
	if a.feishuClient != nil {
		a.feishuClient.Stop()
		log.Println("飞书机器人连接已关闭")
	}
	if a.mcpClient != nil {
		for _, s := range a.mcpClient.ListServers() {
			_ = a.mcpClient.Disconnect(s.ID)
		}
		log.Println("MCP 客户端连接已关闭")
	}
	if a.memoryStore != nil {
		_ = a.memoryStore.Close()
	}
	log.Println("应用已关闭")
}
// evolveDelegate adapts *App to evolvePlugin.Delegate, providing the source
// directory and ACP bridge that the evolve plugin needs.
type evolveDelegate struct {
	a *App
}

func (d evolveDelegate) SourceDir() string      { return d.a.sourceDir }
func (d evolveDelegate) AcpBridge() *acp.Bridge { return d.a.acpBridge }
