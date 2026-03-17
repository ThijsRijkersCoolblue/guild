package llm

import (
	"context"
	"errors"
	"os"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type ClaudeClient struct {
	model string
}

func NewClaudeClient(model string) (LLM, error) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return nil, errors.New("ANTHROPIC_API_KEY is not set")
	}
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	return &ClaudeClient{model: model}, nil
}

func (c *ClaudeClient) Ask(ctx context.Context, prompt string) (string, error) {
	client := anthropic.NewClient(option.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")))

	msg, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 16000,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "", err
	}

	if len(msg.Content) == 0 {
		return "", errors.New("empty response from Claude")
	}

	return msg.Content[0].Text, nil
}
