package procman

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
)

// Restarting supervises a process that is stopped and started again on demand —
// the Go server during hot reload (R1, R2).
//
// The behaviour that matters is what happens on failure. A Go application being
// edited is very often in a state that does not compile, so an exit must not end
// the development session: the process is reported and the supervisor waits for
// the next change. That is the difference between a hot-reload loop that is
// usable while writing code and one that has to be restarted by hand.
type Restarting struct {
	process Process
	logger  *slog.Logger
	stdout  io.Writer
	outMu   sync.Mutex
}

// NewRestarting builds a restartable supervisor.
func NewRestarting(process Process, logger *slog.Logger, stdout io.Writer) *Restarting {
	if logger == nil {
		logger = slog.Default()
	}
	return &Restarting{process: process, logger: logger, stdout: stdout}
}

// Run starts the process and blocks until the context is cancelled. It restarts
// whenever a value arrives on restarts.
func (r *Restarting) Run(ctx context.Context, restarts <-chan struct{}) error {
	for {
		cmd, err := r.start()
		if err != nil {
			// A missing toolchain is not recoverable by editing a file.
			return err
		}

		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()

		select {
		case <-ctx.Done():
			r.stop(cmd)
			return nil

		case <-restarts:
			r.println("change detected, restarting")
			r.stop(cmd)
			// Drain the exit channel so the goroutine does not leak.
			<-exited

		case err := <-exited:
			// The process ended on its own: a compile error, a panic, or a
			// deliberate Ctrl+C inside the child.
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					r.println(fmt.Sprintf(
						"process exited with status %d; waiting for a change to restart",
						exitErr.ExitCode(),
					))
				} else {
					r.println(fmt.Sprintf("process exited: %v; waiting for a change", err))
				}
			} else {
				r.println("process exited; waiting for a change to restart")
			}

			select {
			case <-ctx.Done():
				return nil
			case <-restarts:
				r.println("restarting")
			}
		}
	}
}

// start launches one generation of the process.
func (r *Restarting) start() (*exec.Cmd, error) {
	command, args := splitCommand(r.process)
	cmd := exec.Command(command, args...)
	cmd.Dir = r.process.Dir
	cmd.Env = append(cmd.Environ(), r.process.Env...)

	configureProcess(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf(
			"procman: could not start %s (%s): %w",
			r.process.Name, command, err,
		)
	}

	writer := newPrefixWriter(r.stdout, r.process.Name, r.process.Color, &r.outMu)
	go copyLines(stdout, writer)
	go copyLines(stderr, writer)

	return cmd, nil
}

// stop terminates the process group and waits for it to exit.
func (r *Restarting) stop(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := stopProcess(cmd); err != nil {
		r.logger.Debug("procman: could not stop the process group", "error", err)
	}
	_ = killProcess(cmd)
}

// println writes a supervisor message through the same prefixed writer, so it
// appears in context rather than floating between two processes' output.
func (r *Restarting) println(message string) {
	writer := newPrefixWriter(r.stdout, r.process.Name, r.process.Color, &r.outMu)
	fmt.Fprintln(writer, message)
}

// splitCommand normalises a command that was written as one string.
func splitCommand(process Process) (string, []string) {
	command := process.Command
	args := process.Args
	if len(args) == 0 && containsSpace(command) {
		fields := fields(command)
		if len(fields) > 0 {
			return fields[0], fields[1:]
		}
	}
	return command, args
}

func containsSpace(value string) bool {
	for _, r := range value {
		if r == ' ' {
			return true
		}
	}
	return false
}

// fields splits on whitespace. Hand-rolled to keep this file free of the strings
// import for one call.
func fields(value string) []string {
	var out []string
	current := ""
	for _, r := range value {
		if r == ' ' || r == '\t' {
			if current != "" {
				out = append(out, current)
				current = ""
			}
			continue
		}
		current += string(r)
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}
