//go:build !windows

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"reliquary/internal/types"
)

type processCleaner struct{}

func (processCleaner) Register(types.CleanupFunc)                 {}
func (processCleaner) RegisterWithName(string, types.CleanupFunc) {}
func (processCleaner) Cleanup(context.Context) []error {
	fmt.Println("resources cleaned")
	return nil
}

type failingListener struct{ net.Listener }

func (failingListener) Accept() (net.Conn, error) {
	return nil, errors.New("unexpected accept failure")
}

func TestServerProcessHelper(t *testing.T) {
	scenario := os.Getenv("RELIQUARY_SERVER_PROCESS_TEST")
	if scenario == "" {
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	if scenario == "startup-failure" {
		_, err = listenWithRetry(listener.Addr().String(), 1, time.Millisecond)
	} else {
		if scenario == "runtime-failure" {
			listener = failingListener{listener}
		}
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "ready")
		})}
		if scenario == "server-close" {
			server.Close()
		} else if scenario == "server-shutdown" {
			server.Shutdown(context.Background())
		}
		fmt.Println("ready", listener.Addr())
		err = serveUntilShutdown(server, listener, 3*time.Second, processCleaner{})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func serverProcess(t *testing.T, scenario string) (*exec.Cmd, *bufio.Scanner) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestServerProcessHelper$")
	cmd.Env = append(os.Environ(), "RELIQUARY_SERVER_PROCESS_TEST="+scenario)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	return cmd, bufio.NewScanner(stdout)
}

func TestServerSignalExitsZeroAfterCleanup(t *testing.T) {
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			cmd, output := serverProcess(t, "shutdown")
			var addr string
			for output.Scan() {
				if strings.HasPrefix(output.Text(), "ready ") {
					addr = strings.TrimPrefix(output.Text(), "ready ")
					break
				}
			}
			client := &http.Client{Timeout: 2 * time.Second}
			response, err := client.Get("http://" + addr)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			cleaned := false
			for output.Scan() {
				cleaned = cleaned || strings.Contains(output.Text(), "resources cleaned")
			}
			if err := cmd.Wait(); err != nil || cmd.ProcessState.ExitCode() != 0 || !cleaned {
				t.Fatalf("shutdown: error=%v exit=%d cleaned=%v", err, cmd.ProcessState.ExitCode(), cleaned)
			}
		})
	}
}

func TestServerStartupAndRuntimeFailuresExitNonzero(t *testing.T) {
	for _, scenario := range []string{"startup-failure", "runtime-failure"} {
		t.Run(scenario, func(t *testing.T) {
			cmd, output := serverProcess(t, scenario)
			for output.Scan() {
			}
			if err := cmd.Wait(); err == nil || cmd.ProcessState.ExitCode() == 0 {
				t.Fatalf("%s should fail, error=%v", scenario, err)
			}
		})
	}
}

func TestServerClosedWithoutSignalExitsZeroAfterCleanup(t *testing.T) {
	for _, scenario := range []string{"server-close", "server-shutdown"} {
		t.Run(scenario, func(t *testing.T) {
			cmd, output := serverProcess(t, scenario)
			cleaned := false
			for output.Scan() {
				cleaned = cleaned || strings.Contains(output.Text(), "resources cleaned")
			}
			if err := cmd.Wait(); err != nil || cmd.ProcessState.ExitCode() != 0 || !cleaned {
				t.Fatalf("close: error=%v exit=%d cleaned=%v", err, cmd.ProcessState.ExitCode(), cleaned)
			}
		})
	}
}

func TestExpectedServerCloseRequiresShutdownForListenerErrors(t *testing.T) {
	for _, tc := range []struct {
		err            error
		stopping, want bool
	}{
		{http.ErrServerClosed, false, true},
		{fmt.Errorf("serve: %w", http.ErrServerClosed), false, true},
		{net.ErrClosed, true, true},
		{fmt.Errorf("accept: %w", net.ErrClosed), true, true},
		{errors.New("accept tcp: use of closed network connection"), true, true},
		{net.ErrClosed, false, false},
		{errors.New("use of closed network connection"), false, false},
		{errors.New("unexpected accept failure"), true, false},
	} {
		if got := expectedServerClose(tc.err, tc.stopping); got != tc.want {
			t.Errorf("%v stopping=%v: got %v want %v", tc.err, tc.stopping, got, tc.want)
		}
	}
}
