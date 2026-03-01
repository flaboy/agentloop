package core

import (
	"fmt"
	"strings"
)

func ValidateResponseInputInvariants(input ResponseInput) error {
	if len(input.Items) == 0 {
		return nil
	}

	systemCount := 0
	systemIndex := -1
	seenCallIDs := map[string]struct{}{}

	for i, item := range input.Items {
		itemType := strings.TrimSpace(item.Type)
		role := strings.TrimSpace(item.Role)

		if itemType == "message" && role == "system" {
			systemCount++
			systemIndex = i
		}

		if itemType == "function_call" {
			callID := strings.TrimSpace(item.CallID)
			if callID != "" {
				seenCallIDs[callID] = struct{}{}
			}
		}

		if itemType == "function_call_output" {
			callID := strings.TrimSpace(item.CallID)
			if callID == "" {
				return fmt.Errorf("function_call_output missing call_id at index=%d", i)
			}
			if _, exists := seenCallIDs[callID]; !exists {
				return fmt.Errorf("function_call_output without prior function_call call_id=%q index=%d", callID, i)
			}
		}
	}

	if systemCount > 1 {
		return fmt.Errorf("responses input must contain at most one system message, got=%d", systemCount)
	}
	if systemCount == 1 && systemIndex != 0 {
		return fmt.Errorf("responses input system message must be first, got index=%d", systemIndex)
	}
	return nil
}
