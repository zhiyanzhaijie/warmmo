package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"warmmo/core/internal/application"
	agent "warmmo/core/internal/application/agent"
)

const maxAgentRequestBody = 64 * 1024

type AgentController struct {
	app    *application.AgentService
	logger *slog.Logger
}

type createAgentRunRequest struct {
	Prompt                string   `json:"prompt"`
	ContextNodeIDs        []string `json:"contextNodeIds"`
	Target                string   `json:"target"`
	TargetNodeID          string   `json:"targetNodeId"`
	ProviderID            string   `json:"providerId"`
	ModelID               string   `json:"modelId"`
	ConversationSessionID string   `json:"conversationSessionId"`
}

type respondToAgentRunRequest struct {
	ApprovalEventID string `json:"approvalEventId"`
	Answer          string `json:"answer"`
}

func NewAgentController(agentService *application.AgentService, logger *slog.Logger) *AgentController {
	return &AgentController{app: agentService, logger: logger}
}

func (c *AgentController) CreateRun(response http.ResponseWriter, request *http.Request) {
	var input createAgentRunRequest
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxAgentRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeInvalidRequest(response, "INVALID_REQUEST_BODY", "请求内容无效", err)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeInvalidRequest(response, "INVALID_REQUEST_BODY", "请求只能包含一个 JSON 对象", err)
		return
	}
	run, err := c.app.CreateRun(agent.RunInput{
		WorkID: request.PathValue("workID"), Prompt: input.Prompt, Target: input.Target,
		TargetNodeID: input.TargetNodeID, ProviderID: input.ProviderID, ModelID: input.ModelID,
		ConversationSessionID: input.ConversationSessionID,
		ContextNodeIDs:        input.ContextNodeIDs,
	})
	if err != nil {
		writeAppError(response, c.logger, "create agent run", err)
		return
	}
	writeJSON(response, http.StatusAccepted, run)
}

func (c *AgentController) GetRun(response http.ResponseWriter, request *http.Request) {
	run, err := c.app.GetRun(request.PathValue("runID"))
	if err != nil {
		writeAppError(response, c.logger, "get agent run", err)
		return
	}
	writeJSON(response, http.StatusOK, run)
}

func (c *AgentController) GetConversation(response http.ResponseWriter, request *http.Request) {
	limit := 20
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeInvalidRequest(response, "INVALID_CONVERSATION_LIMIT", "会话数量限制无效", errors.New("limit must be between 1 and 100"))
			return
		}
		limit = parsed
	}
	snapshot, err := c.app.ListConversation(request.Context(), request.PathValue("workID"), limit)
	if err != nil {
		writeAppError(response, c.logger, "list agent conversation", err)
		return
	}
	writeJSON(response, http.StatusOK, snapshot)
}

func (c *AgentController) StreamEvents(response http.ResponseWriter, request *http.Request) {
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeAppError(response, c.logger, "start agent event stream", errors.New("response does not support streaming"))
		return
	}
	afterSequence, err := eventCursor(request)
	if err != nil {
		writeInvalidRequest(response, "INVALID_EVENT_CURSOR", "事件游标无效", err)
		return
	}
	follow := request.URL.Query().Get("follow") == "true"
	if _, err := c.app.GetRun(request.PathValue("runID")); err != nil {
		writeAppError(response, c.logger, "get agent run for stream", err)
		return
	}

	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		events, err := c.app.ListEvents(request.PathValue("runID"), afterSequence)
		if err != nil {
			return
		}
		for _, event := range events {
			encoded, err := json.Marshal(event)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(response, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Type, encoded); err != nil {
				return
			}
			afterSequence = event.Sequence
			flusher.Flush()
		}
		run, err := c.app.GetRun(request.PathValue("runID"))
		if err != nil {
			return
		}
		if len(events) == 0 && isStreamComplete(run.Status) && !follow {
			return
		}
		select {
		case <-request.Context().Done():
			return
		case <-poll.C:
		case <-heartbeat.C:
			if _, err := io.WriteString(response, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (c *AgentController) CancelRun(response http.ResponseWriter, request *http.Request) {
	err := c.app.CancelRun(request.PathValue("runID"))
	if err != nil {
		writeAppError(response, c.logger, "cancel agent run", err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (c *AgentController) RespondToRun(response http.ResponseWriter, request *http.Request) {
	var input respondToAgentRunRequest
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxAgentRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeInvalidRequest(response, "INVALID_AGENT_RESPONSE", "回答内容无效", err)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeInvalidRequest(response, "INVALID_REQUEST_BODY", "请求只能包含一个 JSON 对象", err)
		return
	}
	run, err := c.app.RespondToRun(request.PathValue("runID"), input.ApprovalEventID, input.Answer)
	if err != nil {
		writeAppError(response, c.logger, "respond to agent run", err)
		return
	}
	writeJSON(response, http.StatusAccepted, run)
}

func eventCursor(request *http.Request) (int64, error) {
	value := request.URL.Query().Get("afterSequence")
	if value == "" {
		value = request.Header.Get("Last-Event-ID")
	}
	if value == "" {
		return 0, nil
	}
	sequence, err := strconv.ParseInt(value, 10, 64)
	if err != nil || sequence < 0 {
		return 0, errors.New("invalid event cursor")
	}
	return sequence, nil
}

func isTerminal(status agent.RunStatus) bool {
	return status == agent.RunStatusCompleted || status == agent.RunStatusFailed || status == agent.RunStatusCancelled
}

func isStreamComplete(status agent.RunStatus) bool {
	return isTerminal(status) || status == agent.RunStatusWaitingInput
}
