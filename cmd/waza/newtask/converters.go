package newtask

import (
	"regexp"
	"strings"
	"time"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/go-viper/mapstructure/v2"
	"github.com/microsoft/waza/internal/models"
	"github.com/microsoft/waza/internal/utils"
)

type toolType string

const (
	ToolTypeFunction   = "function"
	ToolTypeBash       = "bash"
	ToolTypePowershell = "powershell"
	ToolTypeView       = "view"
	ToolTypeCreate     = "create"
)

type toolArgs struct {
	Path     string `mapstructure:"path"`      // view, create
	FileText string `mapstructure:"file_text"` // create

	Command     string `mapstructure:"command"`     // bash, powershell
	Description string `mapstructure:"description"` // bash, powershell

	Skill string // skill
}

type tool struct {
	Type toolType

	Start time.Time
	End   time.Time

	Name      string
	Arguments toolArgs
	Success   bool
}

type skill struct {
	Name string
	Path string
}

type CreateTestCaseFromCopilotLogOptions struct {
	DisplayName string
	TestID      string
	Tags        []string
}

func CreateTestCaseFromCopilotLog(copilotLog string, options *CreateTestCaseFromCopilotLogOptions) (*models.TestCase, error) {
	if options == nil {
		options = &CreateTestCaseFromCopilotLogOptions{}
	}

	toolsInOrder := []string{}
	tools := map[string]*tool{}
	var skills []skill

	var displayName string

	if options.DisplayName != "" {
		displayName = options.DisplayName
	}

	var testID string

	if options.TestID != "" {
		testID = options.TestID
	}

	// let's compose a single task
	task := &models.TestCase{
		DisplayName: displayName,
		TestID:      testID,
		Tags:        options.Tags,
	}

	responses := &strings.Builder{}

	for e, err := range utils.NewCopilotLogIterator(copilotLog) {
		if err != nil {
			return nil, err
		}

		switch e.Type {
		case copilot.SessionEventTypeUserMessage:
			if data, ok := e.Data.(*copilot.UserMessageData); ok {
				task.Stimulus.Message = data.Content
			}
		case copilot.SessionEventTypeToolExecutionStart:
			data, ok := e.Data.(*copilot.ToolExecutionStartData)
			if !ok || data.ToolCallID == "" {
				continue
			}

			toolsInOrder = append(toolsInOrder, data.ToolCallID)

			var ta *toolArgs

			if data.Arguments != nil {
				if err := mapstructure.Decode(data.Arguments, &ta); err != nil {
					return nil, err
				}
			} else {
				ta = &toolArgs{}
			}

			toolName := data.ToolName
			if toolName == "" {
				toolName = "<unknown>"
			}

			tools[data.ToolCallID] = &tool{
				Start:     e.Timestamp,
				Name:      toolName,
				Arguments: *ta,
			}
		case copilot.SessionEventTypeToolExecutionComplete:
			data, ok := e.Data.(*copilot.ToolExecutionCompleteData)
			if !ok || data.ToolCallID == "" {
				continue
			}

			t, exists := tools[data.ToolCallID]
			if !exists { // _shouldn't_ happen, but we'll be defensive
				continue
			}

			t.End = e.Timestamp
			t.Success = data.Success
		case copilot.SessionEventTypeAssistantMessage:
			if data, ok := e.Data.(*copilot.AssistantMessageData); ok {
				responses.WriteString(data.Content)
			}
		case copilot.SessionEventTypeAssistantMessageDelta:
			if data, ok := e.Data.(*copilot.AssistantMessageDeltaData); ok {
				responses.WriteString(data.DeltaContent)
			}
		case copilot.SessionEventTypeSkillInvoked:
			data, ok := e.Data.(*copilot.SkillInvokedData)
			if !ok {
				continue
			}

			name := data.Name
			if name == "" {
				name = "<no skill name>"
			}

			path := data.Path
			if path == "" {
				path = "<no path>"
			}

			skills = append(skills, skill{
				Name: name,
				Path: path,
			})
		}
	}

	if len(skills) > 0 {
		var skillNames []string

		for _, sk := range skills {
			skillNames = append(skillNames, sk.Name)
		}

		task.Validators = append(task.Validators, models.ValidatorInline{
			Identifier: "skills-check",
			Kind:       models.GraderKindSkillInvocation,
			Parameters: models.SkillInvocationGraderParameters{
				RequiredSkills: skillNames,
				Mode:           models.SkillMatchingModeAnyOrder,
			},
		})
	}

	if len(tools) > 0 {
		var toolNames []models.ToolSpecParameters

		for _, id := range toolsInOrder {
			if tools[id].Name == "report_intent" {
				continue
			}

			toolNames = append(toolNames, models.ToolSpecParameters{
				Tool:           tools[id].Name,
				CommandPattern: regexp.QuoteMeta(tools[id].Arguments.Command),
				PathPattern:    regexp.QuoteMeta(tools[id].Arguments.Path),
				SkillPattern:   regexp.QuoteMeta(tools[id].Arguments.Skill),
			})
		}

		task.Validators = append(task.Validators, models.ValidatorInline{
			Identifier: "tools-check",
			Kind:       models.GraderKindToolConstraint,
			Parameters: models.ToolConstraintGraderParameters{
				ExpectTools: toolNames,
			},
		})
	}

	task.Validators = append(task.Validators, models.ValidatorInline{
		Identifier: "check-response",
		Kind:       models.GraderKindText,
		Parameters: models.TextGraderParameters{
			ContainsCS: []string{
				responses.String(),
			},
		},
	})

	return task, nil
}
