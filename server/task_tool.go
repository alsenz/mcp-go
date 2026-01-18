package server

import (
	"context"
	"slices"

	"github.com/mark3labs/mcp-go/mcp"
)

// ValidateAnyToolResult tests that a AnyToolResult instance is a valid return value for a tool handler (e.g. CallToolResult or CreateTaskResult)
func ValidateAnyToolResult(value *mcp.AnyToolResult) error {
	if value == nil {
		return ErrInvalidToolResult // tool handlers only return nil with errors
	}
	switch (*value).(type) {
	case mcp.CallToolResult:
		return nil
	case mcp.CreateTaskResult:
		return nil
	}
	return ErrInvalidToolResult
}

// ValidateTaskPayload tests that a TaskPayloadResult is an acceptable payload instance (e.g. CallToolResult)
func ValidateTaskPayload(value mcp.TaskPayloadResult) error {
	switch value.(type) {
	case *mcp.CallToolResult:
		return nil
	}
	return ErrInvalidResultPayload
}

func NewTaskToolAdaptor(handler ToolHandlerFunc) TaskToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.AnyToolResult, error) {
		result, err := handler(ctx, request)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, ErrInvalidToolResult
		}
		resultAsAny := mcp.AnyToolResult(*result)
		return &resultAsAny, nil
	}
}

func NewTaskToolHandlerMiddlewareAdaptor(targetMiddleware ToolHandlerMiddleware) TaskToolHandlerMiddleware {
	return func(next TaskToolHandlerFunc) TaskToolHandlerFunc {
		taskResult := new(*mcp.CreateTaskResult)
		adapted := targetMiddleware(func(ctx context.Context, request mcp.CallToolRequest) (result *mcp.CallToolResult, err error) {
			anyResult, err := next(ctx, request)
			if err != nil {
				return nil, err
			}
			if anyResult != nil {
				switch nextResult := (*anyResult).(type) {
				case mcp.CallToolResult:
					return &nextResult, nil
				case mcp.CreateTaskResult:
					*taskResult = &nextResult
				}
			}
			// Task augmented tool - no CallToolResult, legacy middleware gets pointer to nil value
			return &mcp.CallToolResult{}, nil
		})
		return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.AnyToolResult, error) {
			result, err := adapted(ctx, request)
			if err != nil {
				return nil, err
			}
			if *taskResult != nil {
				var taskResultAsAny mcp.AnyToolResult = **taskResult
				return &taskResultAsAny, nil
			}
			if result == nil {
				return nil, ErrInvalidToolResult
			}
			var toolResultAsAny mcp.AnyToolResult = *result
			return &toolResultAsAny, nil
		}
	}
}

func NewTaskToolBeforeHookAdaptor(hooks *Hooks) OnBeforeAnyToolFunc {
	return func(ctx context.Context, id any, message *mcp.CallToolRequest) {
		hooks.beforeCallTool(ctx, id, message)
	}
}

func NewTaskToolAfterHookAdaptor(hooks *Hooks) OnAfterAnyToolFunc {
	return func(ctx context.Context, id any, message *mcp.CallToolRequest, result *mcp.AnyToolResult) {
		if result != nil {
			if callToolResult, ok := (*result).(mcp.CallToolResult); ok {
				hooks.afterCallTool(ctx, id, message, &callToolResult)
			}
		}
	}
}

// AdaptTools migrates ServerTools into ServerTaskTools with support for task augmentation.
func AdaptTools(tools ...ServerTool) []ServerTaskTool {
	taskTools := make([]ServerTaskTool, 0, len(tools))
	for _, tool := range tools {
		taskTools = append(taskTools, tool.ToTaskTool())
	}
	return taskTools
}

func containTaskAugmented(tools ...ServerTaskTool) bool {
	return slices.IndexFunc(tools, func(tool ServerTaskTool) bool {
		return tool.Tool.Execution.TaskSupport == mcp.ToolTaskSupportOptional || tool.Tool.Execution.TaskSupport == mcp.ToolTaskSupportRequired
	}) >= 0
}
