// Package sandbox provides isolated execution environments for running untrusted scripts.
// It uses Docker containers.
package sandbox

import (
	"context"
	"errors"
	"time"
)

// SandboxType represents the type of sandbox environment
type SandboxType string

const (
	// SandboxTypeDocker runs each session in its own long-lived Docker
	// container, driven through the Docker Engine API. Like the MicroVM
	// backends it keeps session state between executions; unlike them it
	// shares the host kernel and lives on a single daemon.
	SandboxTypeDocker SandboxType = "docker"
	// SandboxTypeDisabled means script execution is disabled
	SandboxTypeDisabled SandboxType = "disabled"
)

// IsNamedSandboxBackendType reports whether raw can be stored as a user-facing
// named Docker backend. It is session-persistent and
// share the same workspace configuration surface.
func IsNamedSandboxBackendType(raw string) bool {
	switch SandboxType(raw) {
	case SandboxTypeDocker:
		return true
	default:
		return false
	}
}

// Default configuration values
const (
	DefaultTimeout     = 60 * time.Second
	DefaultMemoryLimit = 256 * 1024 * 1024 // 256MB
	DefaultCPULimit    = 1.0               // 1 CPU core
	// DefaultDockerImage tracks main rather than latest. The latest tag only
	// moves when a version is released, so it still carries the image from
	// before /workspace and its input/output directories were handed to the
	// sandbox account — a sandbox built from it cannot write its own artifact
	// directory. Point this back at latest once a release ships that fix.
	DefaultDockerImage = "reliquary-sandbox:main"
)

// Common errors
var (
	ErrSandboxDisabled   = errors.New("sandbox is disabled")
	ErrTimeout           = errors.New("execution timed out")
	ErrScriptNotFound    = errors.New("script not found")
	ErrInvalidScript     = errors.New("invalid script")
	ErrExecutionFailed   = errors.New("script execution failed")
	ErrSecurityViolation = errors.New("security validation failed")
	ErrDangerousCommand  = errors.New("script contains dangerous command")
	ErrArgInjection      = errors.New("argument injection detected")
	ErrStdinInjection    = errors.New("stdin injection detected")
)

// Sandbox defines the interface for isolated script execution
type Sandbox interface {
	// Execute runs a script in an isolated environment
	Execute(ctx context.Context, config *ExecuteConfig) (*ExecuteResult, error)

	// Cleanup releases sandbox resources
	Cleanup(ctx context.Context) error

	// Type returns the sandbox type
	Type() SandboxType

	// IsAvailable checks if the sandbox is available for use
	IsAvailable(ctx context.Context) bool
}

// Manager provides a unified interface for sandbox operations
// It handles sandbox selection and fallback logic
type Manager interface {
	// Execute runs a script using the configured sandbox
	Execute(ctx context.Context, config *ExecuteConfig) (*ExecuteResult, error)

	// Cleanup releases all sandbox resources
	Cleanup(ctx context.Context) error

	// GetSandbox returns the active sandbox
	GetSandbox() Sandbox

	// GetType returns the current sandbox type
	GetType() SandboxType
}

// ExecuteConfig contains configuration for script execution
type ExecuteConfig struct {
	// Script is the absolute path to the script file
	Script string

	// Args are command-line arguments to pass to the script
	Args []string

	// WorkDir is the working directory for script execution
	WorkDir string

	// Timeout is the maximum execution time (0 = use default)
	Timeout time.Duration

	// Env is additional environment variables
	Env map[string]string

	// AllowNetwork enables network access (Docker only)
	AllowNetwork bool

	// MemoryLimit is the maximum memory in bytes (Docker only)
	MemoryLimit int64

	// CPULimit is the maximum CPU cores (Docker only)
	CPULimit float64

	// ReadOnlyRootfs makes the root filesystem read-only (Docker only)
	ReadOnlyRootfs bool

	// Stdin provides input to the script
	Stdin string

	// SkipValidation skips security validation (use with caution, only for trusted scripts)
	SkipValidation bool

	// ScriptContent is the script content for validation (optional, will be read from file if not provided)
	ScriptContent string

	// SessionID scopes the execution to a per-session persistent sandbox.
	// When empty, Docker falls back
	// to an ephemeral (one-shot) sandbox created and torn down inside the
	// single Execute call.
	SessionID string

	// RemoteScriptPath is an absolute path to a script that already exists
	// inside the sandbox. When set, the executor skips the upload step and
	// runs it in place. Accepted locations:
	//   - an installed skill file under SkillsImageRoot (bundle vetted at install)
	//   - a session-writable file under /workspace, not under /workspace/input,
	//     which also requires SkillDir so the skill's interpreter is used
	RemoteScriptPath string

	// SkillDir is the installed skill directory whose venv/node_modules
	// should run RemoteScriptPath. Required when RemoteScriptPath sits under
	// /workspace. Image-skill paths derive the directory from the script and
	// ignore this field.
	SkillDir string
}

// ExecuteResult contains the result of script execution
type ExecuteResult struct {
	// Stdout is the standard output from the script
	Stdout string

	// Stderr is the standard error from the script
	Stderr string

	// ExitCode is the process exit code
	ExitCode int

	// Duration is the actual execution time
	Duration time.Duration

	// Killed indicates if the process was killed (e.g., timeout)
	Killed bool

	// Error contains any execution error
	Error string
}

// IsSuccess returns true if the script executed successfully
func (r *ExecuteResult) IsSuccess() bool {
	return r.ExitCode == 0 && !r.Killed && r.Error == ""
}

// Config holds sandbox manager configuration
type Config struct {
	// Type is the preferred sandbox type
	Type SandboxType

	// DefaultTimeout is the default execution timeout
	DefaultTimeout time.Duration

	// AllowPrivateEndpoints is the per-workspace outbound policy for this
	// connection. Link-local addresses are blocked regardless.
	AllowPrivateEndpoints bool

	// DockerImage is the image every sandbox container is created from. It
	// defines the filesystem every session starts from.
	DockerImage string

	// DockerHost is the daemon endpoint, in DOCKER_HOST form
	// ("unix:///var/run/docker.sock", "tcp://10.0.0.5:2376"). Empty uses
	// DefaultDockerHost.
	DockerHost string

	// DockerTLSCertPath is a directory on the Reliquary host holding
	// ca.pem / cert.pem / key.pem. Required for a TCP daemon; unix sockets
	// do not use TLS.
	DockerTLSCertPath string

	// DockerCPULimit / DockerMemoryBytes / DockerPidsLimit cap one sandbox
	// container. Zero uses the built-in defaults.
	DockerCPULimit    float64
	DockerMemoryBytes int64
	DockerPidsLimit   int64

	// DockerNetworkMode is the Docker network every sandbox joins: "bridge" or
	// "none". host, container: and named networks are rejected (see
	// ValidateDockerNetworkMode). Empty means "bridge"; skills that install
	// packages need egress, so a sandbox is not isolated from the network by
	// default.
	DockerNetworkMode string

	// DockerRuntime selects an alternative OCI runtime, e.g. "runsc" for
	// gVisor. Empty uses the daemon's default runtime.
	DockerRuntime string

	// DockerIdleTTL is how long a container may go without executing anything
	// before the idle sweep reclaims it. Zero uses DefaultDockerIdleTTL.
	DockerIdleTTL time.Duration

	// DockerHTTPTimeout bounds each Engine API call. Zero uses the default.
	DockerHTTPTimeout time.Duration

	// MaxMemory is the maximum memory limit in bytes
	MaxMemory int64

	// MaxCPU is the maximum CPU cores
	MaxCPU float64

	// EnvVars are additional environment variables to set for the sandbox.
	EnvVars map[string]string

	// Network is the outbound/inbound policy every sandbox built from this
	// config is created with. DefaultConfig and ResolveEffectiveConfig fully
	// specify it: leaving it nil would let adapters use provider defaults,
	// which expose inbound traffic publicly.
	Network RemoteNetworkPolicy
}

// DefaultConfig returns a default sandbox configuration.
func DefaultConfig() *Config {
	return &Config{
		Type:           SandboxTypeDisabled,
		DefaultTimeout: DefaultTimeout,
		DockerImage:    DefaultDockerImage,
		MaxMemory:      DefaultMemoryLimit,
		MaxCPU:         DefaultCPULimit,
		Network:        resolveNetworkPolicy(nil),
	}
}

// ValidateConfig validates sandbox configuration
func ValidateConfig(config *Config) error {
	if config == nil {
		return errors.New("config is nil")
	}

	switch config.Type {
	case SandboxTypeDocker, SandboxTypeDisabled:
		// Valid types
	default:
		return errors.New("invalid sandbox type")
	}

	if config.DefaultTimeout < 0 {
		return errors.New("timeout cannot be negative")
	}

	if config.MaxMemory < 0 {
		return errors.New("memory limit cannot be negative")
	}

	if config.MaxCPU < 0 {
		return errors.New("CPU limit cannot be negative")
	}

	return nil
}
