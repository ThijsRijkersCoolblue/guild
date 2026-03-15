package main

import (
	"context"
	"guild/llm"
	"guild/tui"
	"log"
)

func main() {
	ctx := context.Background()

	client, err := llm.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	tui.StartChat(ctx, client)
}
