package services

import "github.com/sashabaranov/go-openai"

// NewOpenAIClient returns a client for Chat Completions and Whisper (used by /fill).
func NewOpenAIClient(apiKey string) *openai.Client {
	return openai.NewClient(apiKey)
}
