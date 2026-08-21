package memory

import (
	"context"
	"strings"
	"testing"

	memorymanager "github.com/chhongzh/atri-bot/internal/memory"
	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/cloudwego/eino/components/tool"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestRegisterExposesOnlyMemoryMutations(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	memories := memorymanager.New(db, zap.NewNop())
	if err = memories.Init(); err != nil {
		t.Fatal(err)
	}
	manager := toolmanager.New(db, zap.NewNop())
	if err = Register(manager, memories); err != nil {
		t.Fatal(err)
	}
	if got := manager.Names(); len(got) != 0 {
		t.Fatalf("configurable tools = %v", got)
	}
	tools := manager.Tools()
	if len(tools) != 3 {
		t.Fatalf("registered tools = %d", len(tools))
	}

	ctx := toolmanager.WithRunningState(context.Background(), &toolmanager.RunningState{UserID: 42})
	addTool, ok := tools[0].(tool.InvokableTool)
	if !ok {
		t.Fatalf("add tool type = %T", tools[0])
	}
	result, runErr := addTool.InvokableRun(ctx, `{"content":"用户喜欢阅读"}`)
	if runErr != nil {
		t.Fatal(runErr)
	}
	if strings.Contains(result, "id") {
		t.Fatalf("add result exposes id = %q", result)
	}
}
