package executor

import (
	"bufio"
	pb "github.com/OliveTin/OliveTin/gen/grpc"
	acl "github.com/OliveTin/OliveTin/internal/acl"
	config "github.com/OliveTin/OliveTin/internal/config"
	"github.com/OliveTin/OliveTin/internal/httpservers"
	log "github.com/sirupsen/logrus"
	"io"

	"context"
	"os/exec"
	"runtime"
	"time"
)

// ExecutionRequest is a request to execute an action. It's passed to an
// Executor. They're created from the grpcapi.
type ExecutionRequest struct {
	ActionName         string
	Arguments          map[string]string
	Tags               []string
	action             *config.Action
	Cfg                *config.Config
	AuthenticatedUser  *acl.AuthenticatedUser
	logEntry           *InternalLogEntry
	finalParsedCommand string
}

// InternalLogEntry objects are created by an Executor, and represent the final
// state of execution (even if the command is not executed). It's designed to be
// easily serializable.
type InternalLogEntry struct {
	Datetime string
	Stdout   string
	Stderr   string
	TimedOut bool
	ExitCode int32
	Tags     []string

	/*
		The following two properties are obviously on Action normally, but it's useful
		that logs are lightweight (so we don't need to have an action associated to
		logs, etc. Therefore, we duplicate those values here.
	*/
	ActionTitle string
	ActionIcon  string
}

type executorStepFunc func(*ExecutionRequest) bool

// Executor represents a helper class for executing commands. It's main method
// is ExecRequest
type Executor struct {
	Logs []InternalLogEntry

	chainOfCommand []executorStepFunc
}

// ExecRequest processes an ExecutionRequest
func (e *Executor) ExecRequest(req *ExecutionRequest) *pb.StartActionResponse {
	req.logEntry = &InternalLogEntry{
		Datetime:    time.Now().Format("2006-01-02 15:04:05"),
		ActionTitle: req.ActionName,
		Stdout:      "",
		Stderr:      "",
		ExitCode:    -1337, // If an Action is not actually executed, this is the default exit code.
	}

	for _, step := range e.chainOfCommand {
		if !step(req) {
			break
		}
	}

	e.Logs = append(e.Logs, *req.logEntry)

	return &pb.StartActionResponse{
		LogEntry: &pb.LogEntry{
			ActionTitle: req.logEntry.ActionTitle,
			ActionIcon:  req.logEntry.ActionIcon,
			Datetime:    req.logEntry.Datetime,
			Stderr:      req.logEntry.Stderr,
			Stdout:      req.logEntry.Stdout,
			TimedOut:    req.logEntry.TimedOut,
			ExitCode:    req.logEntry.ExitCode,
		},
	}
}

// DefaultExecutor returns an Executor, with a sensible "chain of command" for
// executing actions.
func DefaultExecutor() *Executor {
	e := Executor{}
	e.chainOfCommand = []executorStepFunc{
		stepFindAction,
		stepACLCheck,
		stepParseArgs,
		stepLogStart,
		stepExec,
		stepLogFinish,
	}

	return &e
}

func stepFindAction(req *ExecutionRequest) bool {
	actualAction := req.Cfg.FindAction(req.ActionName)

	if actualAction == nil {
		log.WithFields(log.Fields{
			"actionName": req.ActionName,
		}).Warnf("Action not found")

		req.logEntry.Stderr = "Action not found"

		return false
	}

	req.action = actualAction
	req.logEntry.ActionIcon = actualAction.Icon

	return true
}

func stepACLCheck(req *ExecutionRequest) bool {
	return acl.IsAllowedExec(req.Cfg, req.AuthenticatedUser, req.action)
}

func stepParseArgs(req *ExecutionRequest) bool {
	var err error

	req.finalParsedCommand, err = parseActionArguments(req.action.Shell, req.Arguments, req.action)

	if err != nil {
		req.logEntry.Stdout = err.Error()

		log.Warnf(err.Error())

		return false
	}

	return true
}

func stepLogStart(req *ExecutionRequest) bool {
	log.WithFields(log.Fields{
		"title":   req.action.Title,
		"timeout": req.action.Timeout,
	}).Infof("Action starting")

	return true
}

func stepLogFinish(req *ExecutionRequest) bool {
	log.WithFields(log.Fields{
		"title":    req.action.Title,
		"stdout":   req.logEntry.Stdout,
		"stderr":   req.logEntry.Stderr,
		"timedOut": req.logEntry.TimedOut,
		"exit":     req.logEntry.ExitCode,
	}).Infof("Action finished")

	return true
}

func wrapCommandInShell(ctx context.Context, finalParsedCommand string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", finalParsedCommand)
	}

	return exec.CommandContext(ctx, "sh", "-c", finalParsedCommand)
}

func stepExec(req *ExecutionRequest) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(req.action.Timeout)*time.Hour)
	defer cancel()

	cmd := wrapCommandInShell(ctx, req.finalParsedCommand)

	var stdout, err = cmd.StdoutPipe()
	if err != nil {
		log.Println(err)
		return false
	}

	var stderr, err2 = cmd.StderrPipe()
	if err2 != nil {
		log.Println(err2)
		return false
	}

	if cmderr := cmd.Start(); cmderr != nil {
		log.Println(cmderr)
		return false
	}

	s := bufio.NewScanner(io.MultiReader(stdout, stderr))
	for s.Scan() {
		httpservers.WsChannel <- s.Text()
	}

	if err := cmd.Wait(); err != nil {
		log.Println(err)
		return false
	}

	if ctx.Err() == context.DeadlineExceeded {
		req.logEntry.TimedOut = true
	}

	req.logEntry.ExitCode = 0
	req.logEntry.Stdout = "Use websocket to get output"
	req.logEntry.Stderr = "Use websocket to get error output"

	req.logEntry.Tags = req.Tags

	return true
}
