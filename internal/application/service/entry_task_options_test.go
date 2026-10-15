package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/config"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

func parseDocumentProcessOpts(t *testing.T, opts []taskqueue.Option) (queue string, timeout time.Duration, maxRetry *int) {
	t.Helper()
	for _, opt := range opts {
		switch opt.Type() {
		case taskqueue.QueueOpt:
			queue, _ = opt.Value().(string)
		case taskqueue.TimeoutOpt:
			timeout, _ = opt.Value().(time.Duration)
		case taskqueue.MaxRetryOpt:
			n, _ := opt.Value().(int)
			maxRetry = &n
		default:
			t.Fatalf("unexpected queue option type %v", opt.Type())
		}
	}
	return queue, timeout, maxRetry
}

func TestDocumentProcessTaskOptions_defaults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  *config.Config
	}{
		{"nil config", nil},
		{"nil library", &config.Config{}},
		{"zero timeout", &config.Config{Library: &config.LibraryConfig{}}},
		{"negative timeout", &config.Config{
			Library: &config.LibraryConfig{DocumentProcessTimeout: -time.Minute},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := documentProcessTaskOptions(tc.cfg)
			queue, timeout, maxRetry := parseDocumentProcessOpts(t, opts)
			assert.Equal(t, types.QueueDefault, queue)
			assert.Equal(t, config.DefaultDocumentProcessTimeout, timeout)
			require.NotNil(t, maxRetry)
			assert.Equal(t, 3, *maxRetry)
		})
	}
}

func TestDocumentProcessTaskOptions_configuredTimeout(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Library: &config.LibraryConfig{
			DocumentProcessTimeout: 90 * time.Minute,
		},
	}
	opts := documentProcessTaskOptions(cfg)
	queue, timeout, maxRetry := parseDocumentProcessOpts(t, opts)
	assert.Equal(t, types.QueueDefault, queue)
	assert.Equal(t, 90*time.Minute, timeout)
	require.NotNil(t, maxRetry)
	assert.Equal(t, 3, *maxRetry)
}

func TestDocumentProcessTaskOptions_extraMaxRetry(t *testing.T) {
	t.Parallel()
	opts := documentProcessTaskOptions(nil, taskqueue.MaxRetry(3))
	queue, timeout, maxRetry := parseDocumentProcessOpts(t, opts)
	assert.Equal(t, types.QueueDefault, queue)
	assert.Equal(t, config.DefaultDocumentProcessTimeout, timeout)
	require.NotNil(t, maxRetry)
	assert.Equal(t, 3, *maxRetry)
}
