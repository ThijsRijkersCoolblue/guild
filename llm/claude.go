package llm

import (
	"context"
	"errors"
	"fmt"
	"os"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type ToolFunc func(input map[string]interface{}) (string, error)

type Tool struct {
	Name        string
	Description string
	InputSchema anthropic.ToolInputSchemaParam
	Handler     ToolFunc
}

type ClaudeClient struct {
	model string
	tools []Tool
}

func NewClaudeClient(model string, tools ...Tool) (LLM, error) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return nil, errors.New("ANTHROPIC_API_KEY is not set")
	}
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	return &ClaudeClient{model: model, tools: tools}, nil
}

func (c *ClaudeClient) Ask(ctx context.Context, prompt string) (string, error) {
	client := anthropic.NewClient(option.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")))

	apiTools := make([]anthropic.ToolUnionParam, len(c.tools))
	for i, t := range c.tools {
		apiTools[i] = anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: t.InputSchema,
			},
		}
	}

	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
	}

	for {
		msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(c.model),
			MaxTokens: 16000,
			Tools:     apiTools,
			Messages:  messages,
		})
		if err != nil {
			return "", fmt.Errorf("API call failed: %w", err)
		}

		messages = append(messages, msg.ToParam())

		if msg.StopReason == "end_turn" {
			for _, block := range msg.Content {
				if block.Type == "text" {
					return block.Text, nil
				}
			}
			return "", errors.New("no text in final response")
		}

		if msg.StopReason == "tool_use" {
			toolResults := []anthropic.ContentBlockParamUnion{}

			for _, block := range msg.Content {
				if block.Type != "tool_use" {
					continue
				}

				result, err := c.dispatchTool(block.Name, block.Input)
				if err != nil {
					toolResults = append(toolResults, anthropic.NewToolResultBlock(
						block.ID, fmt.Sprintf("error: %s", err), true,
					))
				} else {
					toolResults = append(toolResults, anthropic.NewToolResultBlock(
						block.ID, result, false,
					))
				}
			}

			messages = append(messages, anthropic.NewUserMessage(toolResults...))
			continue
		}

		return "", fmt.Errorf("unexpected stop reason: %s", msg.StopReason)
	}
}

func (c *ClaudeClient) dispatchTool(name string, input interface{}) (string, error) {
	inputMap, ok := input.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid input type for tool %s", name)
	}
	for _, t := range c.tools {
		if t.Name == name {
			return t.Handler(inputMap)
		}
	}
	return "", fmt.Errorf("unknown tool: %s", name)
}
