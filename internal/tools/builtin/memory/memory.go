package memory

import (
	"context"

	memorymanager "github.com/chhongzh/atri-bot/internal/memory"
	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
)

const (
	AddMemoryName    = "add_memory"
	DeleteMemoryName = "delete_memory"
	EditMemoryName   = "edit_memory"
)

type addMemoryInput struct {
	Content string `json:"content" jsonschema:"required" jsonschema_description:"一条以“用户”开头的简短、明确、稳定的用户事实。只写事实，不写推测或解释。"`
}

type deleteMemoryInput struct {
	ID uint `json:"id" jsonschema:"required" jsonschema_description:"要删除的记忆 id，来自当前 memory 区块"`
}

type editMemoryInput struct {
	ID      uint   `json:"id" jsonschema:"required" jsonschema_description:"要编辑的记忆 id，来自当前 memory 区块"`
	Content string `json:"content" jsonschema:"required" jsonschema_description:"修改后的简短、明确、稳定的用户事实，应以“用户”开头。"`
}

type memoryMutationResult struct {
	Success bool `json:"success"`
}

// Register adds the memory mutation tools. Memory contents are supplied by a
// dynamic system block, so no list/query tool is exposed.
func Register(manager *toolmanager.Manager, memories *memorymanager.Manager) error {
	addTool, err := toolutils.InferTool(
		AddMemoryName,
		"将一条简短、明确、稳定的用户事实保存到长期记忆。内容必须以“用户”开头，尽量短小精悍，不要保存推测、临时信息、敏感凭据或对话过程。调用成功不会返回 id；下一轮会话会自动注入带 id 的 memory 区块。",
		func(ctx context.Context, input *addMemoryInput) (*memoryMutationResult, error) {
			state, err := toolmanager.RequireRunningState(ctx)
			if err != nil {
				return nil, err
			}
			if err = memories.Add(ctx, state.UserID, input.Content); err != nil {
				return nil, err
			}
			return &memoryMutationResult{Success: true}, nil
		},
	)
	if err != nil {
		return err
	}
	if err = manager.RegisterBuiltin(AddMemoryName, addTool); err != nil {
		return err
	}

	deleteTool, err := toolutils.InferTool(
		DeleteMemoryName,
		"删除当前 memory 区块中指定 id 的长期记忆。只能删除当前用户自己的记忆。调用成功不会返回 id。",
		func(ctx context.Context, input *deleteMemoryInput) (*memoryMutationResult, error) {
			state, err := toolmanager.RequireRunningState(ctx)
			if err != nil {
				return nil, err
			}
			if err = memories.Delete(ctx, state.UserID, input.ID); err != nil {
				return nil, err
			}
			return &memoryMutationResult{Success: true}, nil
		},
	)
	if err != nil {
		return err
	}
	if err = manager.RegisterBuiltin(DeleteMemoryName, deleteTool); err != nil {
		return err
	}

	editTool, err := toolutils.InferTool(
		EditMemoryName,
		"编辑当前 memory 区块中指定 id 的长期记忆。id 必须来自当前 memory 区块，内容应保持简短、明确、以“用户”开头。调用成功不会返回 id。",
		func(ctx context.Context, input *editMemoryInput) (*memoryMutationResult, error) {
			state, err := toolmanager.RequireRunningState(ctx)
			if err != nil {
				return nil, err
			}
			if err = memories.Update(ctx, state.UserID, input.ID, input.Content); err != nil {
				return nil, err
			}
			return &memoryMutationResult{Success: true}, nil
		},
	)
	if err != nil {
		return err
	}
	return manager.RegisterBuiltin(EditMemoryName, editTool)
}
