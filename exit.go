package force

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/gravitational/trace"
)

// Exit exits, if the exit code has been supplied
// it will extract for whatever exit event was sent in the context
func Exit() Action {
	return &SendAction{
		GetEvent: GetExitEventFromContext,
	}
}

type GetEventFunc func(ctx ExecutionContext) Event

func GetExitEventFromContext(ctx ExecutionContext) Event {
	event := &LocalExitEvent{
		created: time.Now().UTC(),
	}
	err := Error(ctx)
	if err == nil {
		return event
	}
	exitErr, ok := trace.Unwrap(err).(*exec.ExitError)
	if ok && exitErr.ProcessState != nil {
		event.Code = exitErr.ProcessState.ExitCode()
	} else {
		event.Code = -1
	}
	return event
}

type SendAction struct {
	GetEvent GetEventFunc
	Process  Process
}

func (e *SendAction) Type() interface{} {
	return 0
}

func (e *SendAction) Eval(ctx ExecutionContext) (interface{}, error) {
	proc := e.Process
	// no process specified? assume broadcast to the process group
	if proc == nil {
		proc = ctx.Process()
	}
	select {
	case proc.Group().BroadcastEvents() <- e.GetEvent(ctx):
		return 0, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

// MarshalCode marshals action to code representation
func (e *SendAction) MarshalCode(ctx ExecutionContext) ([]byte, error) {
	call := &FnCall{Fn: Exit}
	return call.MarshalCode(ctx)
}

// ExitEvent is a special event
// tells the process group to exit with a specified code
type ExitEvent interface {
	ExitCode() int
}

type LocalExitEvent struct {
	Code    int
	created time.Time
}

func (e LocalExitEvent) Created() time.Time {
	return e.created
}

func (e LocalExitEvent) ExitCode() int {
	return e.Code
}

func (e LocalExitEvent) AddMetadata(ctx ExecutionContext) {
}

// String returns a string description of the event
func (e LocalExitEvent) String() string {
	return fmt.Sprintf("Exit(code=%v)", e.Code)
}

// IsExit returns true if it's an exit event
func IsExit(event Event) bool {
	_, ok := event.(ExitEvent)
	return ok
}
