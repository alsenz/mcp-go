package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

//TODO we can do better than this...

// FigureType represents how dark a coffee roast should be used
type RoastType string

const (
	RoastTypeLight  RoastType = "light"
	RoastTypeMedium RoastType = "medium"
	RoastTypeDark   RoastType = "dark"
)

// EspressoArgs defines how the shot should be pulled
type EspressoArgs struct {
	Roast       RoastType `json:"roast"`
	Recipient   string    `json:"recipient"`
	Shots       int       `json:"shots"`
	Temperature float64   `json:"temperature"`
	Preinfusion bool      `json:"preinfusion"`
}

// main starts the MCP-based example server, registers a typed "espresso" tool, and serves it over standard I/O using asynchronous tasks
func main() {
	// Create a new MCP server
	s := server.NewMCPServer(
		"Task Augmented Tool Demo 🚀",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithTaskCapabilities(true, false, true),
	)

	// Add tool with complex schema
	tool := mcp.NewTool("espresso_machine",
		mcp.WithDescription("Make a cup of espresso"),
		mcp.WithString("roast",
			mcp.Description("How the beans should be roasted"),
			mcp.Required(),
			mcp.WithStringEnumItems([]string{string(RoastTypeLight), string(RoastTypeMedium), string(RoastTypeDark)}),
		),
		mcp.WithString("recipient",
			mcp.Description("Name of the person this drink is for"),
		),
		mcp.WithNumber("shots",
			mcp.Description("How many shots should be pulled"),
			mcp.Max(7),
			mcp.Min(1),
			mcp.DefaultNumber(2),
		),
		mcp.WithNumber("temperature",
			mcp.Description("Boiler temperature"),
			mcp.Min(70),
			mcp.Max(98),
		),
		mcp.WithBoolean("preinfusion",
			mcp.Description("Whether the brewing process should include a preinfusion phase"),
		),
	)

	// Add tool handler using the typed handler
	s.AddTaskTool(tool, mcp.NewTypedTaskToolHandler(espressoHandler))

	// Start the stdio server
	if err := server.ServeStdio(s); err != nil {

		fmt.Printf("Server error: %v\n", err)
	}
}

// espressoHandler constructs a personalized greeting from the provided GreetingArgs and returns it as a text tool result.
//
// If args.Name is empty the function returns a tool error result with the message "name is required" and a nil error.
// The returned greeting may include the caller's age, a VIP acknowledgement, the number and list of spoken languages,
// location and timezone from metadata, and a formatted representation of AnyData when present.
func espressoHandler(ctx context.Context, _ mcp.CallToolRequest, args EspressoArgs) (*mcp.AnyToolResult, error) {
	mcpServer := server.ServerFromContext(ctx)

	// Task tools must _immediately_ return task results|Z
	taskCtx, taskId, result := mcpServer.CreateTask(ctx, mcp.WithTaskTTL(int64(1*time.Hour)))
	if args.Roast == "" {
		// Setup errors in handler thread get reported via tasks API.
		if err := mcpServer.FailTask(ctx, taskId, fmt.Errorf("recipient is required")); err != nil {
			return nil, err
		}
	} else {
		go makeEspresso(taskCtx, args, taskId)
	}

	return result.ToAnyResult(), nil
}

func makeEspresso(ctx context.Context, args EspressoArgs, taskId string) {
	mcpServer := server.ServerFromContext(ctx)

	// Let the machine warm up, during which time the order might be cancelled
	time.Sleep(time.Second * time.Duration(args.Temperature/3))
	select {
	case <-ctx.Done(): // Task context cancelled
		return
	case <-time.After(20 * time.Second):
	}

	// Ask for a name
	if args.Recipient == "" {
		request := mcp.ElicitationRequest{
			Params: mcp.ElicitationParams{
				Message: "I need to more information to prepare your espresso drink.",
				RequestedSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"customerName": map[string]any{
							"type":        "string",
							"description": "What is the customer's name?",
							"minLength":   1,
						},
					},
					"required": []string{"customerName"},
				},
			},
		}
		result, err := mcpServer.RequestInput(ctx, taskId, request)
		if err != nil {
			err = errors.Join(err, mcpServer.FailTask(ctx, taskId, err))
			log.Fatalf("error: something went wrong requesting input: %s", err)
		}

		// Handle the customer's response
		switch result.Action {
		case mcp.ElicitationResponseActionCancel:
			if err := mcpServer.CancelTask(ctx, taskId); err != nil {
				log.Printf("warn: unable to cancel task: %s", err)
			}
			return
		case mcp.ElicitationResponseActionDecline:
			args.Recipient = "anonymous customer"
		case mcp.ElicitationResponseActionAccept: // continue
		}

		data, ok := result.Content.(map[string]any)
		if !ok {
			err := fmt.Errorf("unexpected input result type: expected map[string]any, got %T", result.Content)
			err = errors.Join(err, mcpServer.FailTask(ctx, taskId, err))
			log.Printf("error making espresso: %s", err)
		}

		customerName, exists := data["customerName"]
		if !exists {
			err := fmt.Errorf("unexpected input result type: expected map[string]any, got %T", result.Content)
			err = errors.Join(err, mcpServer.FailTask(ctx, taskId, err))
			log.Printf("error making espresso: %s", err)
		}
		args.Recipient, ok = customerName.(string)
		if !ok {
			err := fmt.Errorf("unexpected input result type: expected string, got %T", result.Content)
			err = errors.Join(err, mcpServer.FailTask(ctx, taskId, err))
			log.Printf("error making espresso: %s", err)
		}
	}

	// Go-ahead and pull the shot
	seconds := time.Second * 35
	if args.Preinfusion {
		seconds += 10
	}
	time.Sleep(seconds)

	var result mcp.TaskPayloadResult = mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(fmt.Sprintf("%d shots of %s for %s!", args.Shots, args.Roast, args.Recipient)),
		},
	}
	if err := mcpServer.CompleteTask(ctx, taskId, &result); err != nil {
		err = errors.Join(err, mcpServer.FailTask(ctx, taskId, err))
		log.Fatalf("error: unable to complete task: %s", err)
	}

}
