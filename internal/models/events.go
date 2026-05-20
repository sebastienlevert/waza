package models

import (
	"encoding/json"
	"log/slog"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/go-viper/mapstructure/v2"
)

// ToolCall represents a tool invocation
// captured from tool.execution_* events.
type ToolCall struct {
	Name      string                                   `json:"name"`
	Arguments ToolCallArgs                             `json:"arguments,omitempty"`
	Result    *copilot.ToolExecutionCompleteDataResult `json:"result,omitempty"`
	Success   bool                                     `json:"success"`
}

type ToolCallArgs struct {
	// these are filled out for file-based tools (view/edit)
	Path     string `json:"path"      mapstructure:"path"`
	FileText string `json:"file_text" mapstructure:"file_text"`

	// filled out for tools like bash or powershell
	Command     string `json:"command"     mapstructure:"command"`
	Description string `json:"description" mapstructure:"description"`

	// filled out for skill invocations
	Skill string `json:"skill" mapstructure:"skill"`
}

type TranscriptEventData struct {
	Content    *string                                  `json:"-"`
	Message    *string                                  `json:"-"`
	Arguments  any                                      `json:"-"`
	Success    *bool                                    `json:"-"`
	ToolCallID *string                                  `json:"-"`
	ToolName   *string                                  `json:"-"`
	Result     *copilot.ToolExecutionCompleteDataResult `json:"-"`
}

type TranscriptEvent struct {
	SessionEvent copilot.SessionEvent     `json:"-"`
	Type         copilot.SessionEventType `json:"-"`
	Data         TranscriptEventData      `json:"-"`
}

func NewTranscriptEvent(event copilot.SessionEvent) TranscriptEvent {
	return TranscriptEvent{
		SessionEvent: event,
		Type:         event.Type,
		Data:         extractTranscriptEventData(event),
	}
}

func (te TranscriptEvent) MarshalJSON() ([]byte, error) {
	te = te.normalize()

	v := struct {
		Content *string                  `json:"content,omitempty"`
		Type    copilot.SessionEventType `json:"type"`

		Message *string `json:"message,omitempty"`

		// tool call fields
		Arguments  any                                      `json:"arguments,omitempty"`
		Success    *bool                                    `json:"success,omitempty"`
		ToolCallID *string                                  `json:"tool_call_id,omitempty"`
		ToolName   *string                                  `json:"tool_name,omitempty"`
		ToolResult *copilot.ToolExecutionCompleteDataResult `json:"tool_result,omitempty"`
	}{
		Type:       te.Type,
		Content:    te.Data.Content,
		Message:    te.Data.Message,
		ToolCallID: te.Data.ToolCallID,
		ToolName:   te.Data.ToolName,
		Arguments:  te.Data.Arguments,
		ToolResult: te.Data.Result,
		Success:    te.Data.Success,
	}

	return json.Marshal(v)
}

func (te *TranscriptEvent) UnmarshalJSON(data []byte) error {
	var v struct {
		Content    *string                                  `json:"content,omitempty"`
		Type       copilot.SessionEventType                 `json:"type"`
		Message    *string                                  `json:"message,omitempty"`
		Arguments  any                                      `json:"arguments,omitempty"`
		Success    *bool                                    `json:"success,omitempty"`
		ToolCallID *string                                  `json:"tool_call_id,omitempty"`
		ToolName   *string                                  `json:"tool_name,omitempty"`
		ToolResult *copilot.ToolExecutionCompleteDataResult `json:"tool_result,omitempty"`
	}

	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}

	te.Type = v.Type
	te.Data = TranscriptEventData{
		Content:    v.Content,
		Message:    v.Message,
		Arguments:  v.Arguments,
		Success:    v.Success,
		ToolCallID: v.ToolCallID,
		ToolName:   v.ToolName,
		Result:     v.ToolResult,
	}
	te.SessionEvent = buildSessionEvent(v.Type, te.Data)

	return nil
}

func (te TranscriptEvent) normalize() TranscriptEvent {
	if te.Type == "" {
		te.Type = te.SessionEvent.Type
	}
	if transcriptEventDataIsZero(te.Data) {
		te.Data = extractTranscriptEventData(te.SessionEvent)
	}
	if te.SessionEvent.Type == "" && te.Type != "" {
		te.SessionEvent = buildSessionEvent(te.Type, te.Data)
	}
	return te
}

func transcriptEventDataIsZero(data TranscriptEventData) bool {
	return data.Content == nil &&
		data.Message == nil &&
		data.Arguments == nil &&
		data.Success == nil &&
		data.ToolCallID == nil &&
		data.ToolName == nil &&
		data.Result == nil
}

func extractTranscriptEventData(event copilot.SessionEvent) TranscriptEventData {
	var data TranscriptEventData

	switch d := event.Data.(type) {
	case *copilot.UserMessageData:
		data.Content = ptr(d.Content)
	case *copilot.AssistantMessageData:
		data.Content = ptr(d.Content)
	case *copilot.AssistantReasoningData:
		data.Content = ptr(d.Content)
	case *copilot.SessionErrorData:
		data.Message = ptr(d.Message)
	case *copilot.SessionInfoData:
		data.Message = ptr(d.Message)
	case *copilot.SessionWarningData:
		data.Message = ptr(d.Message)
	case *copilot.SkillInvokedData:
		data.Message = ptr(d.Name)
	case *copilot.ToolUserRequestedData:
		data.Message = ptr(d.ToolName)
		data.ToolCallID = ptr(d.ToolCallID)
		data.ToolName = ptr(d.ToolName)
		data.Arguments = d.Arguments
	case *copilot.ToolExecutionStartData:
		data.ToolCallID = ptr(d.ToolCallID)
		data.ToolName = ptr(d.ToolName)
		data.Arguments = d.Arguments
	case *copilot.ToolExecutionCompleteData:
		data.ToolCallID = ptr(d.ToolCallID)
		data.Success = ptr(d.Success)
		data.Result = d.Result
		if d.Error != nil {
			data.Message = ptr(d.Error.Message)
		}
	case *copilot.ToolExecutionPartialResultData:
		data.ToolCallID = ptr(d.ToolCallID)
		data.Message = ptr(d.PartialOutput)
	case *copilot.HookEndData:
		if d.Error != nil {
			data.Message = ptr(d.Error.Message)
		}
	}

	return data
}

func buildSessionEvent(eventType copilot.SessionEventType, data TranscriptEventData) copilot.SessionEvent {
	event := copilot.SessionEvent{Type: eventType}

	switch eventType {
	case copilot.SessionEventTypeUserMessage:
		if data.Content != nil {
			event.Data = &copilot.UserMessageData{Content: *data.Content}
		}
	case copilot.SessionEventTypeAssistantMessage:
		if data.Content != nil {
			event.Data = &copilot.AssistantMessageData{Content: *data.Content}
		}
	case copilot.SessionEventTypeSessionError:
		if data.Message != nil {
			event.Data = &copilot.SessionErrorData{Message: *data.Message}
		}
	case copilot.SessionEventTypeSkillInvoked:
		name := valueOr(data.Message, "")
		path := ""
		if data.ToolName != nil {
			path = *data.ToolName
		}
		event.Data = &copilot.SkillInvokedData{Name: name, Path: path}
	case copilot.SessionEventTypeToolUserRequested:
		event.Data = &copilot.ToolUserRequestedData{
			ToolCallID: valueOr(data.ToolCallID, ""),
			ToolName:   valueOr(data.ToolName, ""),
			Arguments:  data.Arguments,
		}
	case copilot.SessionEventTypeToolExecutionStart:
		event.Data = &copilot.ToolExecutionStartData{
			ToolCallID: valueOr(data.ToolCallID, ""),
			ToolName:   valueOr(data.ToolName, ""),
			Arguments:  data.Arguments,
		}
	case copilot.SessionEventTypeToolExecutionComplete:
		event.Data = &copilot.ToolExecutionCompleteData{
			ToolCallID: valueOr(data.ToolCallID, ""),
			Success:    valueOr(data.Success, false),
			Result:     data.Result,
		}
	case copilot.SessionEventTypeToolExecutionPartialResult:
		event.Data = &copilot.ToolExecutionPartialResultData{
			ToolCallID:    valueOr(data.ToolCallID, ""),
			PartialOutput: valueOr(data.Message, ""),
		}
	}

	return event
}

// FilterToolCalls goes through the list of session events and correlates tool starts
// with Success.
func FilterToolCalls(sessionEvents []copilot.SessionEvent) []ToolCall {
	toolCallsMap := map[string]*ToolCall{}
	var toolCallIDs []string // preserve the start order of the events.

	for _, evt := range sessionEvents {
		switch evt.Type {
		case copilot.SessionEventTypeToolExecutionStart:
			startData, ok := evt.Data.(*copilot.ToolExecutionStartData)
			if !ok || startData.ToolName == "" || startData.ToolCallID == "" {
				continue
			}

			tc := &ToolCall{
				Name: startData.ToolName,
			}

			if err := mapstructure.Decode(startData.Arguments, &tc.Arguments); err != nil {
				slog.Warn("tool argument format wasn't recognized", "error", err, "name", startData.ToolName, "args", startData.Arguments)
			}

			toolCallsMap[startData.ToolCallID] = tc
			toolCallIDs = append(toolCallIDs, startData.ToolCallID)
		case copilot.SessionEventTypeToolExecutionComplete:
			completeData, ok := evt.Data.(*copilot.ToolExecutionCompleteData)
			if !ok || completeData.ToolCallID == "" {
				continue
			}

			tc := toolCallsMap[completeData.ToolCallID]
			if tc == nil {
				continue
			}

			tc.Success = completeData.Success
			tc.Result = completeData.Result
		}
	}

	var toolCalls []ToolCall

	for _, id := range toolCallIDs {
		toolCalls = append(toolCalls, *toolCallsMap[id])
	}

	return toolCalls
}

func ptr[T any](v T) *T {
	return &v
}

func valueOr[T any](v *T, fallback T) T {
	if v != nil {
		return *v
	}
	return fallback
}


