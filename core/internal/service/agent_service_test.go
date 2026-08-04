package service

import (
	"fmt"
	"testing"

	"warmnote/core/internal/agent"
)

func TestPublicAgentErrorForInvalidDecision(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("run invocation: %w", agent.ErrInvalidDecision)
	message := publicAgentError(err)
	if message != "模型未返回有效的 Agent 决策，请重试或切换模型" {
		t.Fatalf("publicAgentError() = %q", message)
	}
}
