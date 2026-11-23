//go:build unix

package proc

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// forwardedSignals are passed on to a child's process group (and then
// re-raised here). SIGQUIT is left out: re-raising it would make the Go
// runtime dump every goroutine.
var forwardedSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}

// forwardSignals passes the first of forwardedSignals this process receives
// on to process group pgid, then re-raises it on this process with its
// default action -- the same outcome as before the child left the terminal's
// foreground group, when both got it from the terminal directly. Signals
// this process was started with ignored (nohup's SIGHUP, SIGINT for a
// background job) stay ignored. The returned stop must be called once the
// child has exited.
func forwardSignals(pgid int) (stop func()) {
	var sigs []os.Signal
	for _, s := range forwardedSignals {
		if !signal.Ignored(s) {
			sigs = append(sigs, s)
		}
	}
	if len(sigs) == 0 {
		return func() {}
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sigs...)
	quit := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case s := <-ch:
			sig := s.(syscall.Signal)
			_ = syscall.Kill(-pgid, sig)
			signal.Reset(sig)
			_ = syscall.Kill(os.Getpid(), sig)
		case <-quit:
		}
	}()
	return func() {
		signal.Stop(ch)
		close(quit)
		<-finished
	}
}

// kill sends SIGKILL to p, or to its whole process group when group is set
// (the group id is the leader's pid, see setNewGroup).
func kill(p *os.Process, group bool) {
	if group {
		if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err == nil {
			return
		}
	}
	_ = p.Kill()
}

// setNewGroup makes cmd the leader of a new process group.
func setNewGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
