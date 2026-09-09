package llm

import "context"

type Core interface {
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
}
