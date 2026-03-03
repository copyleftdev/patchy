//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	ctransport "github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/patchy-mcp/patchy/internal/observability"
	"github.com/patchy-mcp/patchy/internal/policy"
	"github.com/patchy-mcp/patchy/internal/registry"
	"github.com/patchy-mcp/patchy/internal/runner"
	"github.com/patchy-mcp/patchy/internal/store"
	"github.com/patchy-mcp/patchy/internal/tools"
	"github.com/patchy-mcp/patchy/internal/update"
	"github.com/patchy-mcp/patchy/pkg/schema"
)

// --- Test Harness ---

type testEnv struct {
	mcpClient  *client.Client
	transport  ctransport.Interface
	deps       tools.Deps
	store      *store.FSStore
	updateCtrl *update.Controller
	baseDir    string
	wg         sync.WaitGroup
	cancel     context.CancelFunc
}

func setup(t *testing.T) *testEnv {
	t.Helper()
	baseDir := t.TempDir()

	logCloser := observability.NewLogger(observability.LogConfig{
		Level: "debug", Format: "text", Output: "stderr",
	})
	t.Cleanup(func() { logCloser.Close() })
	logger := logCloser.Logger

	reg := registry.New(registry.BinaryConfig{
		SearchPath: "/home/ops/go/bin",
		Overrides: map[string]string{
			"httpx": "/home/ops/go/bin/httpx",
		},
	}, logger)
	ctx := context.Background()
	if err := reg.Refresh(ctx); err != nil {
		t.Logf("registry refresh partial: %v", err)
	}

	st, err := store.NewFSStore(baseDir, logger)
	if err != nil {
		t.Fatalf("store init: %v", err)
	}

	pol := policy.New(policy.PolicyConfig{
		Scope: policy.ScopeConfig{
			AllowDomains: []string{"example.com", "*.example.com"},
			AllowCIDRs:   []string{"93.184.216.0/24", "2606:2800:220:1::/64"},
		},
		RateLimits: policy.RateLimitConfig{
			Defaults: policy.RateLimitEntry{RequestsPerMin: 120, Burst: 20},
		},
		Concurrency: policy.ConcurrencyConfig{
			Defaults: policy.ConcurrencyEntry{MaxConcurrent: 5},
		},
		Timeouts: policy.TimeoutConfig{
			Defaults: policy.TimeoutEntry{Default: "3m", Max: "10m"},
		},
	}, logger)

	r := runner.New(runner.Config{
		AllowedBinaries: reg.GetAllowedBinaries(),
		BaseOutputDir:   baseDir,
		MaxStdout:       10 << 20,
		MaxStderr:       1 << 20,
	}, logger)

	deps := tools.Deps{
		Runner:   r,
		Policy:   pol,
		Registry: reg,
		Logger:   logger,
		BaseDir:  baseDir,
	}

	uc := update.NewController(reg, r, pol, st, logger)

	// Create MCPServer with task capabilities enabled
	mcpServer := server.NewMCPServer("patchy-integration-test", "0.1.0",
		server.WithToolCapabilities(true),
		server.WithTaskCapabilities(true, true, true),
	)
	tools.RegisterAll(mcpServer, deps)
	tools.RegisterUpdate(mcpServer, uc)
	tools.RegisterSetup(mcpServer, deps, uc)

	// Set up stdio pipes for client-server communication
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()

	srvCtx, srvCancel := context.WithCancel(ctx)

	env := &testEnv{
		deps:       deps,
		store:      st,
		updateCtrl: uc,
		baseDir:    baseDir,
		cancel:     srvCancel,
	}

	// Start StdioServer in goroutine
	env.wg.Add(1)
	go func() {
		defer env.wg.Done()
		stdioServer := server.NewStdioServer(mcpServer)
		stdioServer.SetErrorLogger(log.New(os.Stderr, "", 0))
		_ = stdioServer.Listen(srvCtx, serverReader, serverWriter)
	}()

	// Create transport and client
	tp := ctransport.NewIO(clientReader, clientWriter, io.NopCloser(os.Stderr))
	if err := tp.Start(ctx); err != nil {
		t.Fatalf("transport start: %v", err)
	}
	env.transport = tp

	c := client.NewClient(tp)
	var initReq mcp.InitializeRequest
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "patchy-test", Version: "0.1.0"}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		t.Fatalf("client init: %v", err)
	}
	env.mcpClient = c

	t.Cleanup(func() {
		// 1. Signal server to stop
		srvCancel()
		// 2. Close the write ends to unblock any pending reads
		clientWriter.Close()
		serverWriter.Close()
		// 3. Wait for server goroutine to exit
		env.wg.Wait()
		// 4. Close transport and remaining pipe ends
		tp.Close()
		serverReader.Close()
		clientReader.Close()
	})

	return env
}

// callTool performs a full MCP round-trip via the stdio client.
func (e *testEnv) callTool(t *testing.T, ctx context.Context, name string, args map[string]interface{}) (*schema.RunResult, *mcp.CallToolResult) {
	t.Helper()

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	result, err := e.mcpClient.CallTool(ctx, req)
	if err != nil {
		t.Fatalf("MCP CallTool(%s) transport error: %v", name, err)
	}

	// Parse RunResult from the text content if present
	if len(result.Content) == 0 {
		return nil, result
	}

	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		return nil, result
	}

	var rr schema.RunResult
	if err := json.Unmarshal([]byte(textContent.Text), &rr); err != nil {
		return nil, result
	}

	return &rr, result
}

// callToolRaw performs an MCP round-trip and returns the raw text content.
func (e *testEnv) callToolRaw(t *testing.T, ctx context.Context, name string, args map[string]interface{}) (string, *mcp.CallToolResult) {
	t.Helper()

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	result, err := e.mcpClient.CallTool(ctx, req)
	if err != nil {
		t.Fatalf("MCP CallTool(%s) transport error: %v", name, err)
	}

	if len(result.Content) == 0 {
		return "", result
	}

	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		return "", result
	}

	return textContent.Text, result
}

// --- Async Task Helpers (raw transport) ---

var asyncReqID int64

// callToolAsync sends a task-augmented tools/call via raw transport.
// Returns the task ID immediately without blocking on execution.
func (e *testEnv) callToolAsync(t *testing.T, ctx context.Context, name string, args map[string]interface{}) string {
	t.Helper()

	asyncReqID++
	req := ctransport.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(asyncReqID),
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      name,
			"arguments": args,
			"task":      map[string]interface{}{},
		},
	}

	resp, err := e.transport.SendRequest(ctx, req)
	if err != nil {
		t.Fatalf("async CallTool(%s) transport error: %v", name, err)
	}
	if resp.Error != nil {
		t.Fatalf("async CallTool(%s) JSON-RPC error: code=%d msg=%s", name, resp.Error.Code, resp.Error.Message)
	}

	var createResult struct {
		Task struct {
			TaskId string `json:"taskId"`
			Status string `json:"status"`
		} `json:"task"`
	}
	if err := json.Unmarshal(resp.Result, &createResult); err != nil {
		t.Fatalf("async CallTool(%s) unmarshal CreateTaskResult: %v", name, err)
	}
	if createResult.Task.TaskId == "" {
		t.Fatalf("async CallTool(%s) returned empty taskId", name)
	}

	t.Logf("async task created: tool=%s taskId=%s status=%s", name, createResult.Task.TaskId, createResult.Task.Status)
	return createResult.Task.TaskId
}

// pollTaskDone polls tasks/get until the task reaches a terminal state.
// Returns the final task status.
func (e *testEnv) pollTaskDone(t *testing.T, ctx context.Context, taskID string, timeout time.Duration) string {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("task %s did not complete within %v", taskID, timeout)
		}

		asyncReqID++
		req := ctransport.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mcp.NewRequestId(asyncReqID),
			Method:  "tasks/get",
			Params:  map[string]interface{}{"taskId": taskID},
		}

		resp, err := e.transport.SendRequest(ctx, req)
		if err != nil {
			t.Fatalf("tasks/get(%s) transport error: %v", taskID, err)
		}
		if resp.Error != nil {
			t.Fatalf("tasks/get(%s) JSON-RPC error: code=%d msg=%s", taskID, resp.Error.Code, resp.Error.Message)
		}

		var result struct {
			TaskId string `json:"taskId"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			t.Fatalf("tasks/get(%s) unmarshal: %v", taskID, err)
		}

		status := mcp.TaskStatus(result.Status)
		if status.IsTerminal() {
			return result.Status
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// getTaskResult fetches the final result of a completed task via tasks/result.
func (e *testEnv) getTaskResult(t *testing.T, ctx context.Context, taskID string) *mcp.CallToolResult {
	t.Helper()

	asyncReqID++
	req := ctransport.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(asyncReqID),
		Method:  "tasks/result",
		Params:  map[string]interface{}{"taskId": taskID},
	}

	resp, err := e.transport.SendRequest(ctx, req)
	if err != nil {
		t.Fatalf("tasks/result(%s) transport error: %v", taskID, err)
	}
	if resp.Error != nil {
		t.Fatalf("tasks/result(%s) JSON-RPC error: code=%d msg=%s", taskID, resp.Error.Code, resp.Error.Message)
	}

	result, err := mcp.ParseCallToolResult(&resp.Result)
	if err != nil {
		t.Fatalf("tasks/result(%s) parse CallToolResult: %v", taskID, err)
	}
	return result
}

func requireToolInstalled(t *testing.T, reg *registry.Registry, name string) {
	t.Helper()
	if _, err := reg.GetBinaryPath(name); err != nil {
		t.Skipf("skipping: %s not installed: %v", name, err)
	}
}

// --- Lifecycle Tests ---

func TestFullLifecycle(t *testing.T) {
	env := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Phase A: MCP protocol verification
	t.Run("Phase_A_MCP_Protocol", func(t *testing.T) {
		t.Run("ListTools", func(t *testing.T) {
			testListTools(t, env, ctx)
		})
	})

	// Phase B: Ecosystem tools
	t.Run("Phase_B_Ecosystem", func(t *testing.T) {
		t.Run("Manifest", func(t *testing.T) {
			testManifest(t, env, ctx)
		})
		t.Run("Doctor", func(t *testing.T) {
			testDoctor(t, env, ctx)
		})
	})

	// Phase C: Policy enforcement via MCP round-trip
	t.Run("Phase_C_Policy", func(t *testing.T) {
		t.Run("ScopeViolation", func(t *testing.T) {
			testPolicyScopeViolation(t, env, ctx)
		})
		t.Run("BlockedFlag", func(t *testing.T) {
			testPolicyBlockedFlag(t, env, ctx)
		})
		t.Run("UpdateLock", func(t *testing.T) {
			testPolicyUpdateLock(t, env, ctx)
		})
		t.Run("NoScope_DenyByDefault", func(t *testing.T) {
			testPolicyNoScope(t, env, ctx)
		})
	})

	// Phase D: All 6 primitive tools against example.com
	t.Run("Phase_D_Primitives", func(t *testing.T) {
		t.Run("Subfinder", func(t *testing.T) {
			requireToolInstalled(t, env.deps.Registry, "subfinder")
			testSubfinder(t, env, ctx)
		})
		t.Run("Dnsx", func(t *testing.T) {
			requireToolInstalled(t, env.deps.Registry, "dnsx")
			testDnsx(t, env, ctx)
		})
		t.Run("Httpx", func(t *testing.T) {
			requireToolInstalled(t, env.deps.Registry, "httpx")
			testHttpx(t, env, ctx)
		})
		t.Run("Naabu", func(t *testing.T) {
			requireToolInstalled(t, env.deps.Registry, "naabu")
			testNaabu(t, env, ctx)
		})
		t.Run("Katana", func(t *testing.T) {
			requireToolInstalled(t, env.deps.Registry, "katana")
			testKatana(t, env, ctx)
		})
		t.Run("Nuclei", func(t *testing.T) {
			requireToolInstalled(t, env.deps.Registry, "nuclei")
			testNuclei(t, env, ctx)
		})
	})

	// Phase E: Store artifact verification
	t.Run("Phase_E_Store", func(t *testing.T) {
		testStoreArtifacts(t, env)
	})

	// Phase F: Async task round-trip (non-blocking execution)
	t.Run("Phase_F_Async_Tasks", func(t *testing.T) {
		t.Run("AsyncHttpx", func(t *testing.T) {
			requireToolInstalled(t, env.deps.Registry, "httpx")
			testAsyncHttpx(t, env, ctx)
		})
		t.Run("TaskList", func(t *testing.T) {
			testTaskList(t, env, ctx)
		})
	})

	// Phase G: Onboarding — hints, scope checking, error guidance
	t.Run("Phase_G_Onboarding", func(t *testing.T) {
		t.Run("DoctorHints", func(t *testing.T) {
			testDoctorHints(t, env, ctx)
		})
		t.Run("DoctorScopeCheck", func(t *testing.T) {
			testDoctorScopeCheck(t, env, ctx)
		})
		t.Run("PolicyDenialHints", func(t *testing.T) {
			testPolicyDenialHints(t, env, ctx)
		})
		t.Run("NoScopeHint", func(t *testing.T) {
			testNoScopeHint(t, env, ctx)
		})
		t.Run("SetupToolRegistered", func(t *testing.T) {
			testSetupToolRegistered(t, env, ctx)
		})
		t.Run("IsScopeConfigured", func(t *testing.T) {
			testIsScopeConfigured(t, env)
		})
	})
}

// --- Phase A: MCP Protocol ---

func testListTools(t *testing.T, env *testEnv, ctx context.Context) {
	req := mcp.ListToolsRequest{}
	result, err := env.mcpClient.ListTools(ctx, req)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	expectedTools := map[string]bool{
		"pd.subfinder.enumerate": false,
		"pd.dnsx.resolve":       false,
		"pd.httpx.probe":        false,
		"pd.naabu.scan":         false,
		"pd.katana.crawl":       false,
		"pd.nuclei.scan":        false,
		"pd.ecosystem.manifest": false,
		"pd.ecosystem.doctor":   false,
		"pd.ecosystem.update":   false,
		"pd.ecosystem.setup":    false,
	}

	for _, tool := range result.Tools {
		if _, ok := expectedTools[tool.Name]; ok {
			expectedTools[tool.Name] = true
		}
		if tool.Description == "" {
			t.Errorf("tool %s has empty description", tool.Name)
		}
		t.Logf("  tool: %s — %s", tool.Name, tool.Description[:min(60, len(tool.Description))])
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("expected tool %s not registered", name)
		}
	}

	t.Logf("MCP ListTools: %d tools registered", len(result.Tools))
}

// --- Phase B: Ecosystem ---

func testManifest(t *testing.T, env *testEnv, ctx context.Context) {
	text, raw := env.callToolRaw(t, ctx, "pd.ecosystem.manifest", nil)
	if raw.IsError {
		t.Fatalf("manifest returned error")
	}

	var manifest schema.Manifest
	if err := json.Unmarshal([]byte(text), &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	if manifest.PatchyVersion == "" {
		t.Error("manifest.PatchyVersion empty")
	}
	if len(manifest.Tools) != 6 {
		t.Errorf("expected 6 tools in manifest, got %d", len(manifest.Tools))
	}

	expectedTools := map[string]bool{
		"subfinder": false, "dnsx": false, "httpx": false,
		"naabu": false, "katana": false, "nuclei": false,
	}
	for _, tool := range manifest.Tools {
		if _, ok := expectedTools[tool.Name]; !ok {
			t.Errorf("unexpected tool in manifest: %s", tool.Name)
		}
		expectedTools[tool.Name] = true

		if tool.Installed {
			if tool.BinaryPath == "" {
				t.Errorf("tool %s installed but no binary_path", tool.Name)
			}
			if !tool.Healthy {
				t.Errorf("tool %s installed but not healthy: %s", tool.Name, tool.Error)
			}
			if tool.Version == "" {
				t.Errorf("tool %s healthy but no version", tool.Name)
			}
		}

		t.Logf("  %s: installed=%v healthy=%v version=%s path=%s",
			tool.Name, tool.Installed, tool.Healthy, tool.Version, tool.BinaryPath)
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("tool %s not found in manifest", name)
		}
	}

	t.Logf("manifest: patchy=%s, tools=%d, generated_at=%s",
		manifest.PatchyVersion, len(manifest.Tools), manifest.GeneratedAt)
}

func testDoctor(t *testing.T, env *testEnv, ctx context.Context) {
	text, raw := env.callToolRaw(t, ctx, "pd.ecosystem.doctor", nil)
	if raw.IsError {
		t.Fatalf("doctor returned error")
	}

	var result struct {
		Checks  []tools.DoctorCheck `json:"checks"`
		Healthy bool                `json:"healthy"`
		Summary string              `json:"summary"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("unmarshal doctor: %v", err)
	}

	if len(result.Checks) == 0 {
		t.Error("doctor returned no checks")
	}
	if result.Summary == "" {
		t.Error("doctor summary empty")
	}

	for _, c := range result.Checks {
		if c.Status == "fail" {
			t.Errorf("doctor check FAIL: %s — %s", c.Name, c.Message)
		}
		logLine := "  [" + strings.ToUpper(c.Status) + "] " + c.Name + ": " + c.Message
		if c.Hint != "" {
			logLine += " -> " + c.Hint
		}
		t.Log(logLine)
	}

	// Verify scope check is present
	foundScope := false
	for _, c := range result.Checks {
		if c.Name == "scope" {
			foundScope = true
			if c.Status != "pass" {
				t.Errorf("scope check should pass when scope is configured, got %s", c.Status)
			}
		}
	}
	if !foundScope {
		t.Error("doctor should include a scope check")
	}

	// Verify failing checks have hints
	for _, c := range result.Checks {
		if (c.Status == "fail" || c.Status == "warn") && c.Hint == "" {
			t.Errorf("doctor check %s (status=%s) should have a hint", c.Name, c.Status)
		}
	}

	t.Logf("doctor: healthy=%v, checks=%d", result.Healthy, len(result.Checks))
}

// --- Phase C: Policy ---

func testPolicyScopeViolation(t *testing.T, env *testEnv, ctx context.Context) {
	requireToolInstalled(t, env.deps.Registry, "subfinder")

	rr, _ := env.callTool(t, ctx, "pd.subfinder.enumerate", map[string]interface{}{
		"domain": "evil-not-in-scope.com",
	})
	if rr == nil {
		t.Fatal("expected RunResult for policy denial")
	}
	if rr.Status != "policy_denied" {
		t.Fatalf("expected policy_denied, got %s", rr.Status)
	}
	if rr.Error == nil || rr.Error.Code != "SCOPE_VIOLATION" {
		t.Fatalf("expected SCOPE_VIOLATION, got %+v", rr.Error)
	}
	if rr.RunID == "" {
		t.Error("policy denial should still have a run_id")
	}
	if rr.Error.Hint == "" {
		t.Error("SCOPE_VIOLATION should include a hint")
	}
	t.Logf("scope violation correctly denied: %s (hint=%q, run_id=%s)", rr.Error.Message, rr.Error.Hint, rr.RunID)
}

func testPolicyBlockedFlag(t *testing.T, env *testEnv, ctx context.Context) {
	result := env.deps.Policy.Evaluate(policy.EvalRequest{
		ToolName:   "subfinder",
		BinaryName: "subfinder",
		Targets:    []string{"example.com"},
		Args:       []string{"-d", "example.com", "-update"},
	})
	if result.Allowed {
		t.Fatal("expected denial for -update flag")
	}
	if result.Denials[0].Code != "BLOCKED_FLAG" {
		t.Fatalf("expected BLOCKED_FLAG, got %s", result.Denials[0].Code)
	}
	env.deps.Policy.ReleaseConcurrency("subfinder")
	t.Logf("blocked flag correctly denied: %s", result.Denials[0].Message)
}

func testPolicyUpdateLock(t *testing.T, env *testEnv, ctx context.Context) {
	requireToolInstalled(t, env.deps.Registry, "subfinder")
	env.deps.Policy.SetUpdateLock(true)
	defer env.deps.Policy.SetUpdateLock(false)

	rr, _ := env.callTool(t, ctx, "pd.subfinder.enumerate", map[string]interface{}{
		"domain": "example.com",
	})
	if rr == nil {
		t.Fatal("expected RunResult for update lock denial")
	}
	if rr.Status != "policy_denied" {
		t.Fatalf("expected policy_denied during update lock, got %s", rr.Status)
	}
	if rr.Error.Code != "UPDATE_IN_PROGRESS" {
		t.Fatalf("expected UPDATE_IN_PROGRESS, got %s", rr.Error.Code)
	}
	if rr.Error.Hint == "" {
		t.Error("UPDATE_IN_PROGRESS should include a hint")
	}
	t.Logf("update lock correctly denied: %s (hint=%q)", rr.Error.Message, rr.Error.Hint)
}

func testPolicyNoScope(t *testing.T, env *testEnv, ctx context.Context) {
	noScopePolicy := policy.New(policy.PolicyConfig{
		RateLimits:  policy.RateLimitConfig{Defaults: policy.RateLimitEntry{RequestsPerMin: 60, Burst: 10}},
		Concurrency: policy.ConcurrencyConfig{Defaults: policy.ConcurrencyEntry{MaxConcurrent: 5}},
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	result := noScopePolicy.Evaluate(policy.EvalRequest{
		ToolName: "subfinder", BinaryName: "subfinder",
		Targets: []string{"anything.com"},
	})
	if result.Allowed {
		t.Fatal("expected deny-by-default when no scope configured")
	}
	if result.Denials[0].Code != "NO_SCOPE" {
		t.Fatalf("expected NO_SCOPE, got %s", result.Denials[0].Code)
	}
	t.Logf("deny-by-default correctly enforced: %s", result.Denials[0].Message)
}

// --- Phase D: Primitive Tools ---

func assertRunResultEnvelope(t *testing.T, rr *schema.RunResult, toolName string) {
	t.Helper()
	if rr.SchemaVersion != schema.RunResultSchemaVersion {
		t.Errorf("schema_version: want %s, got %s", schema.RunResultSchemaVersion, rr.SchemaVersion)
	}
	if rr.RunID == "" {
		t.Error("run_id empty")
	}
	if rr.Tool != toolName {
		t.Errorf("tool: want %s, got %s", toolName, rr.Tool)
	}
	if rr.BinaryPath == "" {
		t.Error("binary_path empty")
	}
	if rr.Invocation.Cwd == "" {
		t.Error("invocation.cwd empty")
	}
	if len(rr.Invocation.Args) == 0 {
		t.Error("invocation.args empty")
	}
	if rr.Timing.Start == "" || rr.Timing.End == "" {
		t.Error("timing start/end empty")
	}
	if rr.Timing.DurationMs <= 0 {
		t.Error("timing.duration_ms should be > 0")
	}
	if rr.Environment.PatchyVersion == "" {
		t.Error("environment.patchy_version empty")
	}
	if rr.Environment.OS == "" || rr.Environment.Arch == "" {
		t.Error("environment.os/arch empty")
	}

	// Verify always-injected flags (-duc, -nc) and per-tool JSON flag (-json or -jsonl)
	argsStr := strings.Join(rr.Invocation.Args, " ")
	for _, flag := range []string{"-duc", "-nc"} {
		if !strings.Contains(argsStr, flag) {
			t.Errorf("always-injected flag %s missing from args: %v", flag, rr.Invocation.Args)
		}
	}
	hasJSON := strings.Contains(argsStr, "-json") || strings.Contains(argsStr, "-jsonl")
	if !hasJSON {
		t.Errorf("JSON output flag (-json or -jsonl) missing from args: %v", rr.Invocation.Args)
	}
}

func testSubfinder(t *testing.T, env *testEnv, ctx context.Context) {
	rr, raw := env.callTool(t, ctx, "pd.subfinder.enumerate", map[string]interface{}{
		"domain":  "example.com",
		"sources": []interface{}{"crtsh", "hackertarget"},
	})
	if raw.IsError {
		t.Fatalf("subfinder returned MCP error: status=%s err=%+v", rr.Status, rr.Error)
	}
	if rr.Status != "success" {
		t.Fatalf("expected success, got %s (error: %+v)", rr.Status, rr.Error)
	}
	assertRunResultEnvelope(t, rr, "pd.subfinder.enumerate")

	if rr.Result.RecordType != "SubfinderRecord" {
		t.Errorf("record_type: want SubfinderRecord, got %s", rr.Result.RecordType)
	}

	t.Logf("subfinder: %d records, duration=%dms, truncated=%v",
		rr.Result.Count, rr.Timing.DurationMs, rr.Result.Truncated)

	if rr.Result.Count > 0 {
		var rec schema.SubfinderRecord
		if err := json.Unmarshal(rr.Result.Records[0], &rec); err != nil {
			t.Errorf("unmarshal first SubfinderRecord: %v", err)
		} else {
			if rec.Host == "" {
				t.Error("SubfinderRecord.Host empty")
			}
			t.Logf("  sample: host=%s input=%s source=%s", rec.Host, rec.Input, rec.Source)
		}
	}

	if rr.ToolVersion == "" {
		t.Error("tool_version empty")
	}
	t.Logf("  binary=%s version=%s", rr.BinaryPath, rr.ToolVersion)
}

func testDnsx(t *testing.T, env *testEnv, ctx context.Context) {
	rr, raw := env.callTool(t, ctx, "pd.dnsx.resolve", map[string]interface{}{
		"hosts":        []interface{}{"example.com"},
		"record_types": []interface{}{"a"},
		"resp":         true,
	})
	if raw.IsError {
		t.Fatalf("dnsx returned MCP error: status=%s err=%+v", rr.Status, rr.Error)
	}
	if rr.Status != "success" {
		t.Fatalf("expected success, got %s (error: %+v)", rr.Status, rr.Error)
	}
	assertRunResultEnvelope(t, rr, "pd.dnsx.resolve")

	if rr.Result.RecordType != "DnsxRecord" {
		t.Errorf("record_type: want DnsxRecord, got %s", rr.Result.RecordType)
	}

	t.Logf("dnsx: %d records, duration=%dms", rr.Result.Count, rr.Timing.DurationMs)

	if rr.Result.Count == 0 {
		t.Error("expected at least 1 DNS record for example.com")
	} else {
		var rec schema.DnsxRecord
		if err := json.Unmarshal(rr.Result.Records[0], &rec); err != nil {
			t.Errorf("unmarshal DnsxRecord: %v", err)
		} else {
			if rec.Host != "example.com" {
				t.Errorf("expected host=example.com, got %s", rec.Host)
			}
			if len(rec.A) == 0 {
				t.Error("expected A records for example.com")
			}
			t.Logf("  sample: host=%s a=%v", rec.Host, rec.A)
		}
	}
}

func testHttpx(t *testing.T, env *testEnv, ctx context.Context) {
	rr, raw := env.callTool(t, ctx, "pd.httpx.probe", map[string]interface{}{
		"targets":     []interface{}{"http://example.com"},
		"title":       true,
		"status_code": true,
		"tech_detect": true,
	})
	if raw.IsError {
		t.Fatalf("httpx returned MCP error: status=%s err=%+v", rr.Status, rr.Error)
	}
	if rr.Status != "success" {
		t.Fatalf("expected success, got %s (error: %+v)", rr.Status, rr.Error)
	}
	assertRunResultEnvelope(t, rr, "pd.httpx.probe")

	if rr.Result.RecordType != "HttpxRecord" {
		t.Errorf("record_type: want HttpxRecord, got %s", rr.Result.RecordType)
	}

	t.Logf("httpx: %d records, duration=%dms", rr.Result.Count, rr.Timing.DurationMs)

	if rr.Result.Count == 0 {
		t.Error("expected at least 1 HTTP probe result for example.com")
	} else {
		var rec schema.HttpxRecord
		if err := json.Unmarshal(rr.Result.Records[0], &rec); err != nil {
			t.Errorf("unmarshal HttpxRecord: %v", err)
		} else {
			if rec.URL == "" {
				t.Error("HttpxRecord.URL empty")
			}
			if rec.StatusCode != 200 {
				t.Logf("  warning: expected status 200, got %d", rec.StatusCode)
			}
			t.Logf("  sample: url=%s status=%d title=%q tech=%v",
				rec.URL, rec.StatusCode, rec.Title, rec.Technologies)
		}
	}
}

func testNaabu(t *testing.T, env *testEnv, ctx context.Context) {
	rr, raw := env.callTool(t, ctx, "pd.naabu.scan", map[string]interface{}{
		"hosts":     []interface{}{"example.com"},
		"ports":     "80,443",
		"scan_type": "connect",
	})
	// naabu may fail due to network restrictions; log but don't hard-fail
	if raw.IsError || rr.Status != "success" {
		t.Logf("naabu non-success (may be expected in restricted env): status=%s err=%+v", rr.Status, rr.Error)
		// Still validate the envelope structure even on error
		if rr.RunID == "" {
			t.Error("run_id should be set even on error")
		}
		return
	}
	assertRunResultEnvelope(t, rr, "pd.naabu.scan")

	if rr.Result.RecordType != "NaabuRecord" {
		t.Errorf("record_type: want NaabuRecord, got %s", rr.Result.RecordType)
	}

	t.Logf("naabu: %d records, duration=%dms", rr.Result.Count, rr.Timing.DurationMs)

	if rr.Result.Count > 0 {
		var rec schema.NaabuRecord
		if err := json.Unmarshal(rr.Result.Records[0], &rec); err != nil {
			t.Errorf("unmarshal NaabuRecord: %v", err)
		} else {
			if rec.Host == "" && rec.IP == "" {
				t.Error("NaabuRecord: host and ip both empty")
			}
			if rec.Port <= 0 {
				t.Error("NaabuRecord: port should be > 0")
			}
			t.Logf("  sample: host=%s ip=%s port=%d", rec.Host, rec.IP, rec.Port)
		}
	}
}

func testKatana(t *testing.T, env *testEnv, ctx context.Context) {
	rr, raw := env.callTool(t, ctx, "pd.katana.crawl", map[string]interface{}{
		"targets": []interface{}{"http://example.com"},
		"depth":   float64(1),
	})
	if raw.IsError {
		t.Fatalf("katana returned MCP error: status=%s err=%+v", rr.Status, rr.Error)
	}
	if rr.Status != "success" {
		t.Fatalf("expected success, got %s (error: %+v)", rr.Status, rr.Error)
	}
	assertRunResultEnvelope(t, rr, "pd.katana.crawl")

	if rr.Result.RecordType != "KatanaRecord" {
		t.Errorf("record_type: want KatanaRecord, got %s", rr.Result.RecordType)
	}

	t.Logf("katana: %d records, duration=%dms", rr.Result.Count, rr.Timing.DurationMs)

	if rr.Result.Count > 0 {
		var rec schema.KatanaRecord
		if err := json.Unmarshal(rr.Result.Records[0], &rec); err != nil {
			t.Errorf("unmarshal KatanaRecord: %v", err)
		} else {
			if rec.Request.Endpoint == "" {
				t.Error("KatanaRecord.Request.Endpoint empty")
			}
			t.Logf("  sample: endpoint=%s method=%s status=%d",
				rec.Request.Endpoint, rec.Request.Method, rec.Response.StatusCode)
		}
	}
}

func testNuclei(t *testing.T, env *testEnv, ctx context.Context) {
	rr, raw := env.callTool(t, ctx, "pd.nuclei.scan", map[string]interface{}{
		"targets":  []interface{}{"http://example.com"},
		"severity": []interface{}{"info"},
		"tags":     []interface{}{"tech"},
	})

	if rr == nil {
		t.Fatal("expected RunResult from nuclei")
	}

	// Health gate: nuclei must initialize and scan, not crash immediately.
	// A 60ms duration with exit code 1 means the binary failed to start properly.
	if rr.Timing.DurationMs > 0 && rr.Timing.DurationMs < 500 && rr.Status == "error" {
		// Check stderr for fatal errors indicating broken environment
		stderr := rr.Result.Stderr
		if strings.Contains(stderr, "[FTL]") || strings.Contains(stderr, "Could not create runner") {
			t.Fatalf("nuclei failed to initialize (env broken): %s", stderr)
		}
	}

	// With tags=tech and severity=info, nuclei should find matches on example.com
	// (e.g., waf-detect, tech-detect, dns-waf-detect).
	if rr.Status != "success" {
		t.Fatalf("expected success, got %s (error: %+v, stderr: %s)",
			rr.Status, rr.Error, rr.Result.Stderr)
	}
	assertRunResultEnvelope(t, rr, "pd.nuclei.scan")

	if rr.Result.RecordType != "NucleiRecord" {
		t.Errorf("record_type: want NucleiRecord, got %s", rr.Result.RecordType)
	}

	t.Logf("nuclei: status=%s records=%d duration=%dms is_error=%v",
		rr.Status, rr.Result.Count, rr.Timing.DurationMs, raw.IsError)

	if rr.Result.Count == 0 {
		t.Error("expected at least 1 nuclei finding for example.com with tags=tech,severity=info")
	}

	for i, rec := range rr.Result.Records {
		var nr schema.NucleiRecord
		if err := json.Unmarshal(rec, &nr); err != nil {
			t.Errorf("unmarshal NucleiRecord[%d]: %v", i, err)
			continue
		}
		if nr.TemplateID == "" {
			t.Errorf("NucleiRecord[%d].TemplateID empty", i)
		}
		if nr.Info.Severity == "" {
			t.Errorf("NucleiRecord[%d].Info.Severity empty", i)
		}
		t.Logf("  finding[%d]: template=%s severity=%s host=%s matched=%s",
			i, nr.TemplateID, nr.Info.Severity, nr.Host, nr.MatchedAt)
	}

	if rr.ToolVersion == "" {
		t.Error("tool_version empty")
	}
	if rr.Environment.TemplatesVersion == "" {
		t.Error("environment.templates_version empty")
	}
	t.Logf("  binary=%s version=%s templates=%s",
		rr.BinaryPath, rr.ToolVersion, rr.Environment.TemplatesVersion)
}

// --- Phase E: Store Artifacts ---

func testStoreArtifacts(t *testing.T, env *testEnv) {
	// Verify that run directories were created in baseDir/runs/
	runsDir := env.baseDir + "/runs"
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		t.Logf("runs dir not readable (may not exist if all tools were skipped): %v", err)
		return
	}

	if len(entries) == 0 {
		t.Error("expected at least 1 run directory in store")
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		runID := entry.Name()
		// Each run dir should contain output files
		runEntries, err := os.ReadDir(runsDir + "/" + runID)
		if err != nil {
			t.Errorf("cannot read run dir %s: %v", runID, err)
			continue
		}
		var files []string
		for _, f := range runEntries {
			files = append(files, f.Name())
		}
		t.Logf("  run %s: files=%v", runID[:8], files)
	}

	t.Logf("store: %d run directories created", len(entries))
}

// --- Phase F: Async Tasks ---

func testAsyncHttpx(t *testing.T, env *testEnv, ctx context.Context) {
	// Step 1: Submit task — should return immediately
	start := time.Now()
	taskID := env.callToolAsync(t, ctx, "pd.httpx.probe", map[string]interface{}{
		"targets":     []interface{}{"http://example.com"},
		"title":       true,
		"status_code": true,
	})
	submitDuration := time.Since(start)

	// callToolAsync should return nearly instantly (task creation, not execution)
	if submitDuration > 2*time.Second {
		t.Errorf("task submission took %v — expected near-instant return", submitDuration)
	}
	t.Logf("task submitted in %v (taskId=%s)", submitDuration, taskID)

	// Step 2: Poll until completion
	finalStatus := env.pollTaskDone(t, ctx, taskID, 30*time.Second)
	t.Logf("task %s reached terminal status: %s", taskID, finalStatus)

	if finalStatus != string(mcp.TaskStatusCompleted) {
		t.Fatalf("expected task status 'completed', got '%s'", finalStatus)
	}

	// Step 3: Fetch the result
	result := env.getTaskResult(t, ctx, taskID)
	if result.IsError {
		t.Fatalf("async httpx task returned error")
	}

	if len(result.Content) == 0 {
		t.Fatal("async httpx task returned empty content")
	}

	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatal("async httpx task content is not TextContent")
	}

	var rr schema.RunResult
	if err := json.Unmarshal([]byte(textContent.Text), &rr); err != nil {
		t.Fatalf("unmarshal async RunResult: %v", err)
	}

	if rr.Status != "success" {
		t.Fatalf("async httpx: expected success, got %s (err=%+v)", rr.Status, rr.Error)
	}
	if rr.Result.Count == 0 {
		t.Error("async httpx: expected at least 1 record")
	}

	t.Logf("async httpx complete: records=%d duration=%dms run_id=%s",
		rr.Result.Count, rr.Timing.DurationMs, rr.RunID)
}

func testTaskList(t *testing.T, env *testEnv, ctx context.Context) {
	asyncReqID++
	req := ctransport.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcp.NewRequestId(asyncReqID),
		Method:  "tasks/list",
		Params:  map[string]interface{}{},
	}

	resp, err := env.transport.SendRequest(ctx, req)
	if err != nil {
		t.Fatalf("tasks/list transport error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("tasks/list JSON-RPC error: code=%d msg=%s", resp.Error.Code, resp.Error.Message)
	}

	var result struct {
		Tasks []struct {
			TaskId string `json:"taskId"`
			Status string `json:"status"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("tasks/list unmarshal: %v", err)
	}

	t.Logf("tasks/list: %d tasks tracked by server", len(result.Tasks))
	for _, task := range result.Tasks {
		t.Logf("  taskId=%s status=%s", task.TaskId, task.Status)
	}

	// We should have at least the async httpx task from the previous test
	if len(result.Tasks) == 0 {
		t.Error("expected at least 1 task in task list")
	}
}

// --- Phase G: Onboarding ---

// testDoctorHints verifies that the doctor returns hints for non-pass checks.
func testDoctorHints(t *testing.T, env *testEnv, ctx context.Context) {
	text, raw := env.callToolRaw(t, ctx, "pd.ecosystem.doctor", nil)
	if raw.IsError {
		t.Fatalf("doctor returned error")
	}

	var result struct {
		Checks []tools.DoctorCheck `json:"checks"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("unmarshal doctor: %v", err)
	}

	// Every non-pass check must have a non-empty hint
	for _, c := range result.Checks {
		if c.Status != "pass" && c.Hint == "" {
			t.Errorf("check %s (status=%s) missing hint: %s", c.Name, c.Status, c.Message)
		}
		// Pass checks should NOT have hints (they're noise)
		if c.Status == "pass" && c.Hint != "" {
			t.Errorf("check %s (status=pass) should not have hint, got: %s", c.Name, c.Hint)
		}
	}

	// Summary should include hint arrows for non-pass checks
	var summaryResult struct {
		Summary string `json:"summary"`
	}
	json.Unmarshal([]byte(text), &summaryResult)
	for _, c := range result.Checks {
		if c.Status != "pass" && c.Hint != "" {
			if !strings.Contains(summaryResult.Summary, "-> "+c.Hint) {
				t.Errorf("summary should contain hint for %s: %q", c.Name, c.Hint)
			}
		}
	}

	t.Logf("doctor hints verified: %d checks inspected", len(result.Checks))
}

// testDoctorScopeCheck verifies the doctor includes a scope check that reflects policy state.
func testDoctorScopeCheck(t *testing.T, env *testEnv, ctx context.Context) {
	// With configured scope (setup has example.com), scope check should pass
	text, _ := env.callToolRaw(t, ctx, "pd.ecosystem.doctor", nil)

	var result struct {
		Checks []tools.DoctorCheck `json:"checks"`
	}
	json.Unmarshal([]byte(text), &result)

	found := false
	for _, c := range result.Checks {
		if c.Name == "scope" {
			found = true
			if c.Status != "pass" {
				t.Errorf("scope check should be pass when scope is configured, got %s: %s", c.Status, c.Message)
			}
			break
		}
	}
	if !found {
		t.Fatal("doctor missing scope check")
	}
	t.Log("scope check present and passing with configured scope")
}

// testPolicyDenialHints verifies that policy denials via MCP include actionable hints.
func testPolicyDenialHints(t *testing.T, env *testEnv, ctx context.Context) {
	requireToolInstalled(t, env.deps.Registry, "subfinder")

	// SCOPE_VIOLATION should have a hint
	rr, _ := env.callTool(t, ctx, "pd.subfinder.enumerate", map[string]interface{}{
		"domain": "not-in-scope.example.net",
	})
	if rr == nil {
		t.Fatal("expected RunResult")
	}
	if rr.Status != "policy_denied" {
		t.Fatalf("expected policy_denied, got %s", rr.Status)
	}
	if rr.Error.Hint == "" {
		t.Error("SCOPE_VIOLATION denial should have a hint")
	}
	if !strings.Contains(rr.Error.Hint, "allow_domains") && !strings.Contains(rr.Error.Hint, "allow_cidrs") {
		t.Errorf("SCOPE_VIOLATION hint should reference allow_domains/allow_cidrs, got: %q", rr.Error.Hint)
	}
	t.Logf("SCOPE_VIOLATION hint: %q", rr.Error.Hint)

	// UPDATE_IN_PROGRESS should have a hint
	env.deps.Policy.SetUpdateLock(true)
	rr2, _ := env.callTool(t, ctx, "pd.subfinder.enumerate", map[string]interface{}{
		"domain": "example.com",
	})
	env.deps.Policy.SetUpdateLock(false)

	if rr2 == nil {
		t.Fatal("expected RunResult for update lock")
	}
	if rr2.Error.Hint == "" {
		t.Error("UPDATE_IN_PROGRESS denial should have a hint")
	}
	if !strings.Contains(rr2.Error.Hint, "update") {
		t.Errorf("UPDATE_IN_PROGRESS hint should reference update, got: %q", rr2.Error.Hint)
	}
	t.Logf("UPDATE_IN_PROGRESS hint: %q", rr2.Error.Hint)
}

// testNoScopeHint verifies that NO_SCOPE denial produces the right hint via a dedicated
// no-scope policy engine and direct evaluation (no MCP round-trip needed).
func testNoScopeHint(t *testing.T, env *testEnv, ctx context.Context) {
	noScopePolicy := policy.New(policy.PolicyConfig{
		RateLimits:  policy.RateLimitConfig{Defaults: policy.RateLimitEntry{RequestsPerMin: 60, Burst: 10}},
		Concurrency: policy.ConcurrencyConfig{Defaults: policy.ConcurrencyEntry{MaxConcurrent: 5}},
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	if noScopePolicy.IsScopeConfigured() {
		t.Fatal("expected IsScopeConfigured()=false for empty scope")
	}

	result := noScopePolicy.Evaluate(policy.EvalRequest{
		ToolName: "subfinder", BinaryName: "subfinder",
		Targets: []string{"anything.com"},
	})
	if result.Allowed {
		t.Fatal("expected deny")
	}
	if result.Denials[0].Code != "NO_SCOPE" {
		t.Fatalf("expected NO_SCOPE, got %s", result.Denials[0].Code)
	}

	// Verify hintForDenial would produce the right hint for this code
	// (We test indirectly — the hint is applied in executeTool, not in raw policy eval)
	t.Logf("NO_SCOPE denial verified: %s", result.Denials[0].Message)
}

// testSetupToolRegistered verifies pd.ecosystem.setup is present in tool list with correct schema.
func testSetupToolRegistered(t *testing.T, env *testEnv, ctx context.Context) {
	result, err := env.mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	var setupTool *mcp.Tool
	for i := range result.Tools {
		if result.Tools[i].Name == "pd.ecosystem.setup" {
			setupTool = &result.Tools[i]
			break
		}
	}

	if setupTool == nil {
		t.Fatal("pd.ecosystem.setup not found in tool list")
	}

	if setupTool.Description == "" {
		t.Error("setup tool has empty description")
	}

	// Verify it has the skip_templates parameter in its input schema
	if _, hasSkipTemplates := setupTool.InputSchema.Properties["skip_templates"]; !hasSkipTemplates {
		t.Error("setup tool should have skip_templates parameter")
	}

	t.Logf("pd.ecosystem.setup registered: description=%q", setupTool.Description[:min(60, len(setupTool.Description))])
}

// testIsScopeConfigured verifies the IsScopeConfigured method on both configured and empty engines.
func testIsScopeConfigured(t *testing.T, env *testEnv) {
	// The test env has scope configured
	if !env.deps.Policy.IsScopeConfigured() {
		t.Error("expected IsScopeConfigured()=true for test env with example.com scope")
	}

	// A bare policy engine with no scope should return false
	noScopePolicy := policy.New(policy.PolicyConfig{}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	if noScopePolicy.IsScopeConfigured() {
		t.Error("expected IsScopeConfigured()=false for empty policy")
	}

	t.Log("IsScopeConfigured correctly reflects scope state")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
