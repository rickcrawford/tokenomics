package tokencount

import (
	"fmt"

	"github.com/pkoukk/tiktoken-go"
)

// Count returns the number of tokens in the given text for the specified model.
// Falls back to cl100k_base encoding if the model is not recognized.
func Count(model, text string) (int, error) {
	enc, err := tiktoken.EncodingForModel(model)
	if err != nil {
		// Fall back to cl100k_base (used by gpt-4, gpt-3.5-turbo, etc.)
		enc, err = tiktoken.GetEncoding("cl100k_base")
		if err != nil {
			return 0, fmt.Errorf("get encoding: %w", err)
		}
	}
	tokens := enc.Encode(text, nil, nil)
	return len(tokens), nil
}

// CountMessages estimates token usage for a chat completion messages array.
// Uses the OpenAI token counting heuristic: each message adds overhead tokens.
func CountMessages(model string, messages []map[string]interface{}) (int, error) {
	tokensPerMessage := 3 // Every message follows <|start|>{role/name}\n{content}<|end|>\n
	total := 0

	for _, msg := range messages {
		total += tokensPerMessage
		for _, val := range msg {
			if s, ok := val.(string); ok {
				n, err := Count(model, s)
				if err != nil {
					return 0, err
				}
				total += n
			}
		}
	}
	total += 3 // Every reply is primed with <|start|>assistant<|message|>
	return total, nil
}
