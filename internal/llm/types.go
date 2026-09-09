package llm

type FunctionCall struct {
	Name string `json:"name"`

	Arguments string `json:"arguments"`
}

type ToolCall struct {
	ID string `json:"id"`

	Type string `json:"type"`

	Function FunctionCall `json:"function"`
}

type FunctionDef struct {
	Name string `json:"name"`

	Description string `json:"description"`

	Parameters interface{} `json:"parameters,omitzero"`
}

type ToolDef struct {
	Type string `json:"type"`

	Function FunctionDef `json:"function"`
}

type Message struct {
	Role string `json:"role"`

	Content string `json:"content"`

	ToolCalls []ToolCall `json:"tool_calls,omitzero"`

	ToolCallID string `json:"tool_call_id,omitzero"`

	Name string `json:"name,omitzero"`
}

type ChatRequest struct {
	Messages []Message `json:"messages"`

	Model string `json:"model,omitzero"`

	Temperature float32 `json:"temperature,omitzero"`

	MaxTokens int `json:"max_tokens,omitzero"`

	Stream bool `json:"stream"`

	Tools []ToolDef `json:"tools,omitzero"`
}

type Choice struct {
	Index int `json:"index"`

	Message Message `json:"message"`

	FinishReason string `json:"finish_reason"`
}

type Usage struct {
	PromptTokens int `json:"prompt_tokens"`

	CompletionTokens int `json:"completion_tokens"`

	TotalTokens int `json:"total_tokens"`
}

type ChatResponse struct {
	ID string `json:"id"`

	Model string `json:"model"`

	Object string `json:"object"`

	Created int64 `json:"created"`

	Choices []Choice `json:"choices"`

	Usage Usage `json:"usage"`
}

type StreamChoice struct {
	Index int `json:"index"`

	Delta Message `json:"delta"`

	FinishReason string `json:"finish_reason"`
}

type StreamChunk struct {
	ID string `json:"id"`

	Model string `json:"model"`

	Object string `json:"object"`

	Created int64 `json:"created"`

	Choices []StreamChoice `json:"choices"`
}

type ModelCapabilities struct {
	IsSupportTool bool `json:"is_support_tool"`

	IsSupportVision bool `json:"is_support_vision"`

	IsSupportJSONMode bool `json:"is_support_json_mode"`

	IsSupportStreaming bool `json:"is_support_streaming"`

	IsSupportFunctionCalling bool `json:"is_support_function_calling"`
}

type ModelLimits struct {
	MaxContextTokens int `json:"max_context_tokens"`

	MaxOutputTokens int `json:"max_output_tokens"`

	MaxToolCalls int `json:"max_tool_calls"`
}

type ModelPricing struct {
	InputPricePer1K float64 `json:"input_price_per_1k"`

	OutputPricePer1K float64 `json:"output_price_per_1k"`

	Currency string `json:"currency"`
}

type Model struct {
	ID string `json:"id"`

	Name string `json:"name"`

	Vendor string `json:"vendor"`

	Capabilities ModelCapabilities `json:"capabilities"` 

	Limits ModelLimits `json:"limits"`

	Pricing ModelPricing `json:"pricing"`
}
