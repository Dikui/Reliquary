package sandbox

import (
	"errors"
	"fmt"
	"testing"
)

func TestRemoteBindingDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		replace  bool
		preserve bool
	}{
		{name: "nil", err: nil},
		{
			name:     "not found",
			err:      NewRemoteError(SandboxTypeDocker, "Get", RemoteErrorKindNotFound, "gone", nil),
			replace:  true,
			preserve: false,
		},
		{
			name:     "terminal",
			err:      NewRemoteError(SandboxTypeDocker, "Get", RemoteErrorKindTerminal, "terminated", nil),
			replace:  true,
			preserve: false,
		},
		{
			name:     "authentication",
			err:      NewRemoteError(SandboxTypeDocker, "Connect", RemoteErrorKindAuthentication, "denied", nil),
			preserve: true,
		},
		{
			name:     "invalid request",
			err:      NewRemoteError(SandboxTypeDocker, "Create", RemoteErrorKindInvalidRequest, "bad template", nil),
			preserve: true,
		},
		{
			name:     "unsupported",
			err:      NewRemoteError(SandboxTypeDocker, "Connect", RemoteErrorKindUnsupported, "no reconnect", nil),
			preserve: true,
		},
		{
			name:     "conflict",
			err:      NewRemoteError(SandboxTypeDocker, "Delete", RemoteErrorKindConflict, "busy", nil),
			preserve: true,
		},
		{
			name:     "capacity",
			err:      NewRemoteError(SandboxTypeDocker, "Create", RemoteErrorKindCapacity, "full", nil),
			preserve: true,
		},
		{
			name:     "timeout",
			err:      NewRemoteError(SandboxTypeDocker, "Get", RemoteErrorKindTimeout, "deadline", nil),
			preserve: true,
		},
		{
			name:     "unavailable",
			err:      NewRemoteError(SandboxTypeDocker, "Health", RemoteErrorKindUnavailable, "offline", nil),
			preserve: true,
		},
		{
			name:     "internal",
			err:      NewRemoteError(SandboxTypeDocker, "List", RemoteErrorKindInternal, "unknown", nil),
			preserve: true,
		},
		{
			name:     "wrapped terminal",
			err:      fmt.Errorf("probe failed: %w", NewRemoteError(SandboxTypeDocker, "Get", RemoteErrorKindTerminal, "dead", nil)),
			replace:  true,
			preserve: false,
		},
		{
			name:     "unclassified defaults to preserve",
			err:      errors.New("provider returned an unknown error"),
			preserve: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := CanReplaceRemoteBinding(tt.err); got != tt.replace {
				t.Fatalf("CanReplaceRemoteBinding() = %v, want %v", got, tt.replace)
			}
		})
	}
}

func TestRemoteErrorRetainsCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection reset")
	err := NewRemoteError(
		SandboxTypeDocker,
		"Connect",
		RemoteErrorKindUnavailable,
		"control plane unavailable",
		cause,
	)

	if !errors.Is(err, cause) {
		t.Fatal("RemoteError must retain its provider-native cause")
	}
	if got, want := err.Error(), "docker Connect: unavailable: control plane unavailable: connection reset"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestRemoteErrorDiagnostics(t *testing.T) {
	t.Parallel()

	remote := NewRemoteError(
		SandboxTypeDocker, "Exec", RemoteErrorKindAuthentication,
		"HTTP 403", errors.New("forbidden"),
	)
	remote.StatusCode = 403
	got := RemoteErrorDiagnostics(remote)
	if got != "authentication op=Exec http=403 HTTP 403" {
		t.Fatalf("RemoteErrorDiagnostics() = %q", got)
	}
	if RemoteErrorDiagnostics(errors.New("plain")) != "plain" {
		t.Fatal("expected plain error text")
	}
}

func TestIsRemoteDirAlreadyExists(t *testing.T) {
	t.Parallel()

	directoryErr := NewRemoteError(
		SandboxTypeDocker, "MakeDir", RemoteErrorKindInternal,
		"failed to make dir /opt/reliquary/tenant/skills/sk-1: directory already exists: /opt/reliquary/tenant/skills/sk-1",
		nil,
	)
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "directory already exists", err: directoryErr, want: true},
		{
			name: "wrapped directory error",
			err:  fmt.Errorf("sandbox: create install directory: %w", directoryErr),
			want: true,
		},
		{
			name: "directory conflict",
			err: NewRemoteError(
				SandboxTypeDocker, "MakeDir", RemoteErrorKindConflict, "already exists", nil,
			),
			want: true,
		},
		{
			name: "unrelated internal",
			err:  NewRemoteError(SandboxTypeDocker, "MakeDir", RemoteErrorKindInternal, "disk full", nil),
			want: false,
		},
		{
			name: "conflict without already exists",
			err:  NewRemoteError(SandboxTypeDocker, "Delete", RemoteErrorKindConflict, "busy", nil),
			want: false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsRemoteDirAlreadyExists(tt.err); got != tt.want {
				t.Fatalf("IsRemoteDirAlreadyExists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsRemoteConflict(t *testing.T) {
	t.Parallel()

	if IsRemoteConflict(nil) {
		t.Fatal("nil is not a conflict")
	}
	conflict := NewRemoteError(SandboxTypeDocker, "DeleteSnapshot", RemoteErrorKindConflict, "busy", nil)
	if !IsRemoteConflict(conflict) {
		t.Fatal("Conflict kind must match")
	}
	invalid := NewRemoteError(
		SandboxTypeDocker, "DeleteSnapshot", RemoteErrorKindInvalidRequest, "bad id", nil,
	)
	if IsRemoteConflict(invalid) {
		t.Fatal("InvalidRequest must not match")
	}
}
