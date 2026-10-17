// Package procman supervises the child processes behind `vuepher dev` (R1).
//
// The problem it solves is concrete: `vuepher dev` must run the Go server and the
// Vite dev server together, keep their output legible in one terminal, and — the
// part that is easy to get wrong — not leak processes when the user presses
// Ctrl+C or when one child dies. A stray Vite process holding port 5173 is the
// most common way a hot-reload setup becomes unusable.
//
// All children are started in their own process group where the platform allows
// it, so terminating the group takes grandchildren with it (a Go rebuild spawns
// the compiled binary as a child of `go run`).
package procman

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Process describes one child process.
type Process struct {
	// Name is the label prefixed to the process's output.
	Name string

	// Color is an ANSI colour name: "blue", "green", "yellow", "magenta", "cyan".
	Color string

	// Command is the executable.
	Command string

	// Args are the arguments, with a leading command name if that is how the
	// caller wrote it.
	Args []string

	// Dir is the working directory. Empty means the current directory.
	Dir string

	// Env holds extra environment variables in KEY=VALUE form.
	Env []string

	// Ready, when set, is called once with the process's first line of output, so
	// the supervisor can wait for a service to be listening before starting a
	// dependent process (R1: the Go server must not serve before Vite is up).
	Ready func(line string)
}

// Supervisor runs a set of child processes as one unit.
type Supervisor struct {
	processes []Process
	stdout    io.Writer
	logger    *slog.Logger

	mu      sync.Mutex
	started []*exec.Cmd

	// outMu serialises writes from every child, so two processes writing at the
	// same moment cannot interleave inside a line.
	outMu sync.Mutex
}

// New builds a supervisor.
func New(logger *slog.Logger, stdout io.Writer, processes ...Process) *Supervisor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Supervisor{processes: processes, stdout: stdout, logger: logger}
}

// Run starts every process and blocks until one exits or the context is
// cancelled, then stops the rest.
//
// It returns the error from the process that ended the run, so a failing build
// surfaces as a non-zero exit rather than a clean shutdown.
func (s *Supervisor) Run(ctx context.Context) error {
	if len(s.processes) == 0 {
		return errors.New("procman: no processes to run")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, len(s.processes))

	for _, process := range s.processes {
		cmd, err := s.start(process)
		if err != nil {
			s.stopAll()
			return err
		}
		go func(process Process, cmd *exec.Cmd) {
			err := cmd.Wait()
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					errCh <- fmt.Errorf("%s exited with status %d", process.Name, exitErr.ExitCode())
					return
				}
			}
			errCh <- nil
		}(process, cmd)
	}

	select {
	case <-ctx.Done():
		s.stopAll()
		return nil
	case err := <-errCh:
		// One child ending means the session is over. Stopping the rest keeps the
		// terminal usable.
		s.stopAll()
		return err
	}
}

// start launches one process and wires its output through the prefixed writer.
func (s *Supervisor) start(process Process) (*exec.Cmd, error) {
	command := process.Command
	args := process.Args
	// Callers write commands the way they would on a shell: "go run ." is
	// Command "go", Args ["run", "."]. Accept the whole string in Command too.
	if strings.Contains(command, " ") && len(args) == 0 {
		fields := strings.Fields(command)
		command, args = fields[0], fields[1:]
	}

	cmd := exec.Command(command, args...)
	cmd.Dir = process.Dir
	cmd.Env = append(cmd.Environ(), process.Env...)

	configureProcess(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("procman: could not capture %s output: %w", process.Name, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("procman: could not capture %s errors: %w", process.Name, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf(
			"procman: could not start %s (%s %s): %w",
			process.Name, command, strings.Join(args, " "), err,
		)
	}

	s.mu.Lock()
	s.started = append(s.started, cmd)
	s.mu.Unlock()

	writer := newPrefixWriter(s.stdout, process.Name, process.Color, &s.outMu)
	go s.pipe(stdout, writer, process)
	go s.pipe(stderr, writer, process)

	return cmd, nil
}

// pipe forwards a stream to the prefixed writer, line by line, so output from two
// processes does not interleave mid-line.
func (s *Supervisor) pipe(reader io.Reader, writer io.Writer, process Process) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	readyNotified := false
	for scanner.Scan() {
		line := scanner.Text()
		// The prefix writer emits the newline itself, so the line is passed
		// without one to avoid a blank line after every entry.
		fmt.Fprintf(writer, "%s\n", line)

		if !readyNotified && process.Ready != nil {
			readyNotified = true
			process.Ready(line)
		}
	}
}

// stopAll terminates every child, waiting briefly for a graceful exit before
// killing the process group.
func (s *Supervisor) stopAll() {
	s.mu.Lock()
	commands := make([]*exec.Cmd, len(s.started))
	copy(commands, s.started)
	s.started = nil
	s.mu.Unlock()

	for _, cmd := range commands {
		if cmd.Process == nil {
			continue
		}
		// Ask the whole process group first, so a `go run` wrapper and the
		// binary it spawned both stop.
		if err := stopProcess(cmd); err != nil {
			s.logger.Debug("procman: could not stop a process group", "error", err)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	for _, cmd := range commands {
		if cmd.Process == nil {
			continue
		}
		for time.Now().Before(deadline) {
			if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		_ = killProcess(cmd)
	}
}

// WaitFor blocks until check returns true or the timeout expires. It is used to
// hold the Go server back until the Vite dev server is listening.
func WaitFor(ctx context.Context, timeout time.Duration, check func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}
	return check()
}
