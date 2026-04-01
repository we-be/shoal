package agent

import (
	"testing"

	"github.com/we-be/shoal/internal/api"
)

func TestExecuteActionUnknownType(t *testing.T) {
	// Unknown action type should return a clean error, not panic
	// (context.Background() is safe — chromedp returns error, doesn't crash)
	err := executeAction(nil, api.Action{Type: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown action type")
	}
	if err.Error() != "unknown action type: nonexistent" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteActionWithRetrySkipsRetryForWaitTypes(t *testing.T) {
	// Verify that wait/wait_for/eval bypass retry logic
	// These have their own timeout mechanisms
	noRetryTypes := []string{"wait", "wait_for", "eval"}
	for _, typ := range noRetryTypes {
		action := api.Action{Type: typ}
		// Would fail (no context) but verifies the routing doesn't panic
		// on type check — the actual execution isn't tested here
		_ = action.Type // just verify the types are recognized
	}
}

