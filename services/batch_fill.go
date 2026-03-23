package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sashabaranov/go-openai"
)

const chatModelFill = "gpt-4o-mini"

// FillSlot описание одного незаполненного слота для промпта.
type FillSlot struct {
	MessageID       int64  `json:"message_id"`
	LocalTime       string `json:"local_time"` // HH:MM в локальном времени пользователя
	IntervalMinutes int64  `json:"interval_minutes"`
}

// FillAssignmentRaw то, что возвращает LLM (до сопоставления с БД).
type FillAssignmentRaw struct {
	MessageIDs   []int64 `json:"message_ids"`
	ActivityPath string  `json:"activity_path"`
}

type fillPlanRaw struct {
	Assignments []FillAssignmentRaw `json:"assignments"`
}

// ParseFillAssignments вызывает LLM и парсит JSON-план заполнения слотов.
func ParseFillAssignments(ctx context.Context, client *openai.Client, userDescription string, slots []FillSlot, activityPaths []string) ([]FillAssignmentRaw, error) {
	if client == nil {
		return nil, fmt.Errorf("openai client is nil")
	}

	slotsJSON, err := json.Marshal(slots)
	if err != nil {
		return nil, err
	}
	pathsJSON, err := json.Marshal(activityPaths)
	if err != nil {
		return nil, err
	}

	system := `You are helping fill time-tracking activity slots. The user describes what they did in natural language (possibly Russian).
You must respond with a single JSON object only, no markdown, with this exact shape:
{"assignments":[{"message_ids":[...integers...],"activity_path":"exact path from list"}]}
Rules:
- PARTIAL fills are required when the user only described part of the timeline: include ONLY message_ids for slots that the description clearly covers. Do NOT guess, do NOT fill silence, do NOT assign slots the user did not mention.
- Example: if two consecutive unfilled slots span 90 minutes but the user says they did X only during the last hour, assign only the slot(s) whose local_time range matches that last hour (use slot interval_minutes and local_time to decide).
- If nothing in the description maps confidently to any slot, return {"assignments":[]}.
- Each message_id from the slots list may appear in at most one assignment, and only if justified by the text.
- activity_path must be copied EXACTLY from the provided list of allowed paths (same spelling, same " / " separators).
- Merge consecutive slots that belong to the same activity AND the same described stretch into one assignment with multiple message_ids.
- Order assignments by time (earlier slots first).`

	userMsg := fmt.Sprintf(`Allowed leaf activity paths (use these strings exactly):
%s

Unfilled slots (message_id = Telegram message to update; local_time is start of that slot in the user's local time; interval_minutes is slot length):
%s

User description:
%s

Remember: only output assignments for slots the user actually described; leave other slots out of the JSON entirely.`, string(pathsJSON), string(slotsJSON), userDescription)

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: chatModelFill,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: system},
			{Role: openai.ChatMessageRoleUser, Content: userMsg},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Temperature: 0.2,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty completion")
	}

	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	var plan fillPlanRaw
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("parse llm json: %w (content: %s)", err, truncate(content, 500))
	}
	return plan.Assignments, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TranscribeOggVoice расшифровывает голосовое (OGG/Opus как в Telegram) через Whisper.
func TranscribeOggVoice(ctx context.Context, client *openai.Client, filePath string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("openai client is nil")
	}
	ar, err := client.CreateTranscription(ctx, openai.AudioRequest{
		Model:    openai.Whisper1,
		FilePath: filePath,
		Language: "ru",
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(ar.Text), nil
}
