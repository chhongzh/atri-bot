package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chhongzh/atri-bot/internal/account"
	mcpmanager "github.com/chhongzh/atri-bot/internal/mcp"
	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/cloudwego/eino/components/tool"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func newTestManagers(t *testing.T) (*toolmanager.Manager, *mcpmanager.Manager) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	accounts := account.New(db, zap.NewNop(), 36)
	if err = accounts.Init(); err != nil {
		t.Fatal(err)
	}
	mcpManager := mcpmanager.New(context.Background(), zap.NewNop(), db, accounts, mcpmanager.Config{BlockInternal: false})
	if err = mcpManager.Init(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 42} {
		if _, _, err = accounts.EnsureUser(context.Background(), id, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	toolManager := toolmanager.New(db, zap.NewNop())
	if err = toolManager.Init(); err != nil {
		t.Fatal(err)
	}
	if err = Register(toolManager, mcpManager); err != nil {
		t.Fatal(err)
	}
	return toolManager, mcpManager
}

func TestMCPToolManagementFlow(t *testing.T) {
	toolManager, _ := newTestManagers(t)
	ctx := toolmanager.WithRunningState(context.Background(), &toolmanager.RunningState{UserID: 42})

	addOutput := invoke(t, toolManager, AddMCPProviderName, ctx,
		`{"name":"git","url":"https://git.example/sse","meta":{"timeout":30},"header":{"Authorization":"Bearer secret"}}`)
	var addResult mcpProviderValueResult
	if err := json.Unmarshal([]byte(addOutput), &addResult); err != nil {
		t.Fatal(err)
	}
	if addResult.Value != "https://git.example/sse" {
		t.Fatalf("add result = %#v", addResult)
	}

	listOutput := invoke(t, toolManager, ListMCPProvidersName, ctx, `{}`)
	var listResult listMCPProvidersResult
	if err := json.Unmarshal([]byte(listOutput), &listResult); err != nil {
		t.Fatal(err)
	}
	if len(listResult.Providers) != 1 {
		t.Fatalf("providers = %#v", listResult.Providers)
	}
	if listResult.Providers[0].Name != "git" || len(listResult.Providers[0].HeaderKeys) != 1 {
		t.Fatalf("provider summary = %#v", listResult.Providers[0])
	}

	getOutput := invoke(t, toolManager, GetMCPProviderValueName, ctx, `{"name":"git","path":"header.Authorization"}`)
	var getResult mcpProviderValueResult
	if err := json.Unmarshal([]byte(getOutput), &getResult); err != nil {
		t.Fatal(err)
	}
	if getResult.Value != "********" {
		t.Fatalf("get result = %#v", getResult)
	}

	updateOutput := invoke(t, toolManager, UpdateMCPProviderName, ctx,
		`{"name":"git","path":"header.Authorization","value":"Bearer new"}`)
	var updateResult mcpProviderValueResult
	if err := json.Unmarshal([]byte(updateOutput), &updateResult); err != nil {
		t.Fatal(err)
	}
	if updateResult.Value != "********" {
		t.Fatalf("update result = %#v", updateResult)
	}

	removeOutput := invoke(t, toolManager, RemoveMCPProviderName, ctx, `{"name":"git"}`)
	var removeResult removeMCPProviderResult
	if err := json.Unmarshal([]byte(removeOutput), &removeResult); err != nil {
		t.Fatal(err)
	}
	if removeResult.Removed != "git" {
		t.Fatalf("remove result = %#v", removeResult)
	}
}

func TestMCPGateIsPermissionOnly(t *testing.T) {
	toolManager, _ := newTestManagers(t)
	if !toolManager.HasPermission(GateToolName) {
		t.Fatalf("gate permission %q not registered", GateToolName)
	}
	if toolManager.Has(GateToolName) {
		t.Fatalf("gate permission %q must not be callable", GateToolName)
	}
	for _, name := range []string{ListMCPProvidersName, GetMCPProviderValueName, AddMCPProviderName, UpdateMCPProviderName, RemoveMCPProviderName} {
		permission, ok := toolManager.PermissionName(name)
		if !ok || permission != GateToolName {
			t.Fatalf("permission for %q = %q, %v; want %q", name, permission, ok, GateToolName)
		}
	}
}

func TestRedactProviderURLMasksQuery(t *testing.T) {
	got := redactProviderURL("https://example.com/sse?token=secret&mode=full")
	if got != "https://example.com/sse?redacted" {
		t.Fatalf("redacted URL = %q", got)
	}
	if got := redactProviderURL("https://example.com/sse"); got != "https://example.com/sse" {
		t.Fatalf("URL without query changed to %q", got)
	}
}

func invoke(t *testing.T, manager *toolmanager.Manager, name string, ctx context.Context, arguments string) string {
	t.Helper()
	result, err := invokableTool(t, manager, name).InvokableRun(ctx, arguments)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func invokableTool(t *testing.T, manager *toolmanager.Manager, name string) tool.InvokableTool {
	t.Helper()
	for _, candidate := range manager.Tools() {
		info, err := candidate.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name == name {
			invokable, ok := candidate.(tool.InvokableTool)
			if !ok {
				t.Fatalf("tool %s is not invokable", name)
			}
			return invokable
		}
	}
	t.Fatalf("tool %s not found", name)
	return nil
}
