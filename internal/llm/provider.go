package llm

type Provider interface {
	Name() string

	DefaultModel() *Model

	ConvertRequest(req *ChatRequest) ([]byte, error)

	ConvertResponse(data []byte) (*ChatResponse, error)

	ConvertStreamChunk(data []byte) (*StreamChunk, error)

	IsStreamDone(data []byte) bool

	Headers() map[string]string

	BaseURL() string
}


