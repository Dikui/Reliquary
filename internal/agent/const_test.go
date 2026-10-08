package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestToolExecutionTimeout(t *testing.T) {
	assert.Equal(t, 60*time.Second, defaultToolExecTimeout)
}
