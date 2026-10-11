package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"reliquary/internal/utils"
	"sort"
	"strconv"
	"strings"
)

const (
	pipelineLogValueMaxRune = 300
	defaultPipelineStage    = "PIPELINE"
	defaultPipelineAction   = "info"
	pipelineLogPrefix       = "[PIPELINE]"
	pipelineTruncateEll     = "..."
)

// PipelineLog builds a structured pipeline log string.
func PipelineLog(stage, action string, fields map[string]interface{}) string {
	if stage == "" {
		stage = defaultPipelineStage
	}
	if action == "" {
		action = defaultPipelineAction
	}

	builder := strings.Builder{}
	builder.Grow(128)
	builder.WriteString(pipelineLogPrefix)
	builder.WriteString(" stage=")
	builder.WriteString(stage)
	builder.WriteString(" action=")
	builder.WriteString(action)

	if len(fields) > 0 {
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			builder.WriteString(" ")
			builder.WriteString(key)
			builder.WriteString("=")
			builder.WriteString(utils.SanitizeForLog(formatPipelineLogValue(fields[key])))
		}
	}
	return builder.String()
}

// PipelineInfo logs pipeline info level entries.
func PipelineInfo(ctx context.Context, stage, action string, fields map[string]interface{}) {
	GetLogger(ctx).Info(PipelineLog(stage, action, fields))
}

// PipelineWarn logs pipeline warning level entries.
func PipelineWarn(ctx context.Context, stage, action string, fields map[string]interface{}) {
	GetLogger(ctx).Warn(PipelineLog(stage, action, fields))
}

// PipelineError logs pipeline error level entries.
func PipelineError(ctx context.Context, stage, action string, fields map[string]interface{}) {
	GetLogger(ctx).Error(PipelineLog(stage, action, fields))
}

func formatPipelineLogValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strconv.Quote(truncatePipelineValue(v))
	case fmt.Stringer:
		return strconv.Quote(truncatePipelineValue(v.String()))
	case json.RawMessage:
		bytes, _ := v.MarshalJSON()
		return string(bytes)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func truncatePipelineValue(content string) string {
	content = strings.ReplaceAll(content, "\n", "\\n")
	runes := []rune(content)
	if len(runes) <= pipelineLogValueMaxRune {
		return content
	}
	return string(runes[:pipelineLogValueMaxRune]) + pipelineTruncateEll
}

func TruncateForLog(content string) string {
	return truncatePipelineValue(content)
}
