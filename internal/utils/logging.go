package utils

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	copilot "github.com/github/copilot-sdk/go"
)

// NewSessionToSlog creates a function compatible with [copilot.Session.On] that will
// emit log entries, to slog, when the log level is set to slog.LevelDebug.
func NewSessionToSlog() copilot.SessionEventHandler {
	if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		return func(copilot.SessionEvent) {}
	}

	intentCalls := sync.Map{}

	return func(event copilot.SessionEvent) {
		switch event.Type {
		case copilot.SessionEventTypePendingMessagesModified,
			copilot.SessionEventTypeHookEnd,
			copilot.SessionEventTypeHookStart:
			// we just drop these from logging, they're mostly noise, or have other events (like tool calls)
			// that are more informative.
			return
		case copilot.SessionEventTypeToolExecutionStart:
			if data, ok := event.Data.(*copilot.ToolExecutionStartData); ok && data.ToolName == "report_intent" {
				// store this off, we'll ignore the complete event when it comes in as well.
				intentCalls.Store(data.ToolCallID, true)
				return
			}
		case copilot.SessionEventTypeToolExecutionComplete:
			if data, ok := event.Data.(*copilot.ToolExecutionCompleteData); ok &&
				intentCalls.CompareAndDelete(data.ToolCallID, true) {
				return
			}
		}

		sessionToSlog(event)
	}
}

// sessionToSlog tries to be a low-overhead method for dumping out any session events coming from
// the copilot client to slog. It's safe to add this to your copilot session instances, in
// their [copilot.Session.On] handler.
func sessionToSlog(event copilot.SessionEvent) {
	if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		return
	}

	attrs := []any{
		"type", event.Type,
	}

	switch data := event.Data.(type) {
	case *copilot.SessionStartData:
		attrs = appendIf(attrs, "selectedModel", data.SelectedModel)
		attrs = append(attrs, "producer", data.Producer)
		attrs = append(attrs, "sessionID", data.SessionID)

		if data.Context != nil {
			cc := data.Context
			var ccAttrs []any

			ccAttrs = appendIf(ccAttrs, "branch", cc.Branch)
			ccAttrs = append(ccAttrs, "cwd", cc.Cwd)
			ccAttrs = appendIf(ccAttrs, "gitRoot", cc.GitRoot)
			ccAttrs = appendIf(ccAttrs, "repository", cc.Repository)

			attrs = append(attrs, slog.Group("context", ccAttrs...))
		}
	case *copilot.AssistantTurnStartData:
		attrs = append(attrs, "turnID", data.TurnID)
	case *copilot.AssistantMessageData:
		attrs = append(attrs, "content", data.Content)
		attrs = appendIf(attrs, "reasoningText", data.ReasoningText)
	case *copilot.AssistantMessageDeltaData:
		attrs = append(attrs, "deltaContent", data.DeltaContent)
	case *copilot.AssistantReasoningData:
		attrs = append(attrs, "content", data.Content)
	case *copilot.AssistantReasoningDeltaData:
		attrs = append(attrs, "deltaContent", data.DeltaContent)
	case *copilot.UserMessageData:
		attrs = append(attrs, "content", data.Content)
	case *copilot.ToolExecutionStartData:
		attrs = append(attrs, "toolName", data.ToolName, "toolCallID", data.ToolCallID)
		attrs = appendMapOfStringAnyIf(attrs, data.Arguments, "arguments")
	case *copilot.ToolExecutionCompleteData:
		attrs = append(attrs, "toolCallID", data.ToolCallID, "success", data.Success)
		if data.Result != nil {
			var toolResultArgs []any
			toolResultArgs = append(toolResultArgs, "content", data.Result.Content)
			toolResultArgs = appendIf(toolResultArgs, "detailedContent", data.Result.DetailedContent)
			attrs = append(attrs, slog.Group("toolResult", toolResultArgs...))
		}
		if data.Error != nil {
			attrs = append(attrs, "message", data.Error.Message)
		}
	case *copilot.ToolExecutionPartialResultData:
		attrs = append(attrs, "toolCallID", data.ToolCallID, "partialOutput", data.PartialOutput)
	case *copilot.HookStartData:
		attrs = append(attrs, "hookType", data.HookType)
		attrs = appendMapOfStringAnyIf(attrs, data.Input, "input")
	case *copilot.HookEndData:
		attrs = append(attrs, "hookType", data.HookType, "success", data.Success)
		if data.Error != nil {
			attrs = append(attrs, "message", data.Error.Message)
		}
	case *copilot.SessionErrorData:
		attrs = append(attrs, "message", data.Message)
	case *copilot.SkillInvokedData:
		attrs = append(attrs, "message", data.Name)
	default:
		if data != nil {
			attrs = append(attrs, "data", fmt.Sprintf("%T", data))
		}
	}

	slog.Debug("Event received", attrs...)
}

// appendIf appends the attribute if v is not nil
func appendIf[T any](attrs []any, name string, v *T) []any {
	if v != nil {
		attrs = append(attrs, name)
		attrs = append(attrs, *v)
	}

	return attrs
}

// appendMapOfStringAnyIf appends the contents of the map, as a slog.Group if the
// map is both a map[string]any, and not empty.
// NOTE: the keys are not sorted as they are added to the slog.Group.
func appendMapOfStringAnyIf(attrs []any, mapOfStringAny any, fieldName string) []any {
	if asMap, ok := mapOfStringAny.(map[string]any); ok {
		if len(asMap) == 0 {
			return attrs
		}

		var args []any

		for k, v := range asMap {
			args = append(args, k, v)
		}

		attrs = append(attrs, slog.Group(fieldName, args...))
	}

	return attrs
}
