package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	agent "warmnote/core/internal/agent/writing"
	"warmnote/core/internal/application"
)

const maxAgentRequestBody = 64 * 1024

type AgentController struct {
	app    *application.AgentService
	logger *slog.Logger
}

type createAgentRunRequest struct {
	Prompt         string   `json:"prompt"`
	ContextNodeIDs []string `json:"contextNodeIds"`
	Target         string   `json:"target"`
	TargetNodeID   string   `json:"targetNodeId"`
	ProviderID     string   `json:"providerId"`
	ModelID        string   `json:"modelId"`
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
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "请求内容无效"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "请求只能包含一个 JSON 对象"})
		return
	}
	run, err := c.app.CreateRun(agent.RunInput{
		WorkID: request.PathValue("workID"), Prompt: input.Prompt, Target: input.Target,
		TargetNodeID: input.TargetNodeID, ProviderID: input.ProviderID, ModelID: input.ModelID,
		ContextNodeIDs: input.ContextNodeIDs,
	})
	if err != nil {
		if errors.Is(err, application.ErrInvalidAgentRun) {
			writeJSON(response, http.StatusBadRequest, map[string]string{"message": strings.TrimPrefix(err.Error(), application.ErrInvalidAgentRun.Error()+": ")})
			return
		}
		c.internalError(response, "create agent run", err)
		return
	}
	writeJSON(response, http.StatusAccepted, run)
}

func (c *AgentController) GetRun(response http.ResponseWriter, request *http.Request) {
	run, err := c.app.GetRun(request.PathValue("runID"))
	if errors.Is(err, agent.ErrRunNotFound) {
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "Agent Run 不存在"})
		return
	}
	if err != nil {
		c.internalError(response, "get agent run", err)
		return
	}
	writeJSON(response, http.StatusOK, run)
}

func (c *AgentController) StreamEvents(response http.ResponseWriter, request *http.Request) {
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"message": "事件流不可用"})
		return
	}
	afterSequence, err := eventCursor(request)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "事件游标无效"})
		return
	}
	follow := request.URL.Query().Get("follow") == "true"
	if _, err := c.app.GetRun(request.PathValue("runID")); errors.Is(err, agent.ErrRunNotFound) {
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "Agent Run 不存在"})
		return
	} else if err != nil {
		c.internalError(response, "get agent run for stream", err)
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
	switch {
	case errors.Is(err, agent.ErrRunNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "Agent Run 不存在"})
	case errors.Is(err, agent.ErrRunNotCancellable):
		writeJSON(response, http.StatusConflict, map[string]string{"message": "Agent Run 已结束，无法取消"})
	case err != nil:
		c.internalError(response, "cancel agent run", err)
	default:
		response.WriteHeader(http.StatusNoContent)
	}
}

func (c *AgentController) RespondToRun(response http.ResponseWriter, request *http.Request) {
	var input respondToAgentRunRequest
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxAgentRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "回答内容无效"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "请求只能包含一个 JSON 对象"})
		return
	}
	run, err := c.app.RespondToRun(request.PathValue("runID"), input.ApprovalEventID, input.Answer)
	switch {
	case errors.Is(err, agent.ErrRunNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "Agent Run 不存在"})
	case errors.Is(err, agent.ErrRunNotWaitingInput), errors.Is(err, agent.ErrInvalidUserResponse):
		writeJSON(response, http.StatusConflict, map[string]string{"message": "这个问题已经回答或不再有效"})
	case errors.Is(err, application.ErrInvalidAgentRun):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": strings.TrimPrefix(err.Error(), application.ErrInvalidAgentRun.Error()+": ")})
	case err != nil:
		c.internalError(response, "respond to agent run", err)
	default:
		writeJSON(response, http.StatusAccepted, run)
	}
}

func (c *AgentController) internalError(response http.ResponseWriter, operation string, err error) {
	c.logger.Error(operation, "error", err)
	writeJSON(response, http.StatusInternalServerError, map[string]string{"message": "Agent 服务不可用"})
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
