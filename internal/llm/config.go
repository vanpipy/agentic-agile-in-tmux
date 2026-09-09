package llm

type VendorConfig struct {
	APIKey string `json:"api_key"`

	BaseURL string `json:"base_url"`

	Timeout int `json:"timeout,omitzero"`

	MaxRetries int `json:"max_retries,omitzero"`

	DefaultModel Model `json:"default_model,omitzero"`

	Models []Model `json:"models"`
}

type Config struct {
	DefaultModel Model `json:"default_model"`

	Providers []Provider `json:"Providers"`
}
