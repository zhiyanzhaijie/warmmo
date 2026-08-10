package adk

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	warmagent "warmmo/core/internal/adapter/agent/writing"
)

const appName = "warmmo"

type ModelResolver func(context.Context, string, string) (ModelConfig, error)

type Engine struct {
	loop       *warmagent.Loop
	resolve    ModelResolver
	httpClient *http.Client
}

func NewEngine(loop *warmagent.Loop, resolve ModelResolver) *Engine {
	return &Engine{
		loop:       loop,
		resolve:    resolve,
		httpClient: &http.Client{Timeout: 4 * time.Minute},
	}
}

func (e *Engine) Run(ctx context.Context, input warmagent.RunInput, emit warmagent.Emitter) (warmagent.RunResult, error) {
	if e.loop == nil || e.resolve == nil {
		return warmagent.RunResult{}, fmt.Errorf("ADK engine is not configured")
	}
	modelConfig, err := e.resolve(ctx, input.ProviderID, input.ModelID)
	if err != nil {
		return warmagent.RunResult{}, fmt.Errorf("resolve model: %w", err)
	}
	textModel := NewTextModel(modelConfig, e.httpClient)

	var result warmagent.RunResult
	customAgent, err := agent.New(agent.Config{
		Name:        "warmmo_agent",
		Description: "Runs the explicit Warmmo novel-writing loop.",
		Run: func(invocation agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				var runErr error
				result, runErr = e.loop.Run(invocation, input, textModel, emit)
				if runErr != nil {
					yield(nil, runErr)
					return
				}
				event := session.NewEventWithContext(invocation, invocation.InvocationID())
				event.Content = genai.NewContentFromText(result.Content, genai.RoleModel)
				yield(event, nil)
			}
		},
	})
	if err != nil {
		return warmagent.RunResult{}, fmt.Errorf("create ADK custom agent: %w", err)
	}

	adkRunner, err := runner.New(runner.Config{
		AppName:           appName,
		Agent:             customAgent,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
	})
	if err != nil {
		return warmagent.RunResult{}, fmt.Errorf("create ADK runner: %w", err)
	}
	message := genai.NewContentFromText(input.Prompt, genai.RoleUser)
	for _, runErr := range adkRunner.Run(ctx, "local-author", input.RunID, message, agent.RunConfig{}) {
		if runErr != nil {
			return warmagent.RunResult{}, fmt.Errorf("run ADK invocation: %w", runErr)
		}
	}
	return result, nil
}
