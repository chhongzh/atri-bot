// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package exit

import (
	"context"
	"testing"

	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"
)

func TestExitToolInterruptsAndLogsTrace(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	manager := toolmanager.New(db, zap.New(core))
	if err = Register(manager, zap.New(core)); err != nil {
		t.Fatal(err)
	}
	tools := manager.Tools()
	if len(tools) != 1 {
		t.Fatalf("registered tools = %d, want 1", len(tools))
	}
	exitTool, ok := tools[0].(tool.InvokableTool)
	if !ok {
		t.Fatalf("exit tool type = %T", tools[0])
	}

	ctx := toolmanager.WithRunningState(context.Background(), &toolmanager.RunningState{
		UserID:      42,
		CharacterID: "dev.example.character",
	})
	_, err = exitTool.InvokableRun(ctx, "{}")
	if _, interrupted := compose.IsInterruptRerunError(err); !interrupted {
		t.Fatalf("exit error = %v, want interrupt", err)
	}
	entries := logs.FilterMessage("model exited chat turn without a reply").All()
	if len(entries) != 1 {
		t.Fatalf("exit trace entries = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["user_id"] != int64(42) || fields["character_id"] != "dev.example.character" {
		t.Fatalf("exit trace fields = %#v", fields)
	}
}
