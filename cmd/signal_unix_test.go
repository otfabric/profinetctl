// SPDX-License-Identifier: MIT

//go:build !windows

package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// signalChild set to 1 makes TestSecondSignalEndsTheProcess play, in the
// process it starts, a command that does not watch its context.
const signalChild = "PROFINETCTL_TEST_SIGNAL_CHILD"

// The first SIGINT cancels the context of Execute; a later one ends the
// process although the command has not returned.
func TestSecondSignalEndsTheProcess(t *testing.T) {
	if os.Getenv(signalChild) == "1" {
		ctx, stop := signalContext(context.Background())
		defer stop()
		fmt.Println("ready")
		<-ctx.Done()
		fmt.Println("cancelled")
		// The command goes on as one that takes no context would.
		time.Sleep(time.Minute)
		os.Exit(3)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSecondSignalEndsTheProcess$")
	cmd.Env = append(os.Environ(), signalChild+"=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := bufio.NewScanner(stdout)
	expect := func(want string) {
		t.Helper()
		if !lines.Scan() || lines.Text() != want {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("the process wrote %q, want %q (%v)", lines.Text(), want, lines.Err())
		}
	}
	expect("ready")
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	expect("cancelled")
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	// The default action comes back once the context is cancelled: SIGINT
	// is sent until it ends the process.
	again := time.NewTicker(20 * time.Millisecond)
	defer again.Stop()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case err := <-done:
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				t.Fatalf("the process ended with %v, want an end by SIGINT", err)
			}
			if ws, ok := ee.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGINT {
				t.Fatalf("the process ended with %v, want an end by SIGINT", err)
			}
			return
		case <-again.C:
			_ = cmd.Process.Signal(syscall.SIGINT)
		case <-deadline.C:
			_ = cmd.Process.Kill()
			t.Fatalf("a second SIGINT did not end the process (killed: %v)", <-done)
		}
	}
}
