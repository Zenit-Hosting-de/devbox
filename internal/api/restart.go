package api

import (
	"time"
)

// Notice of the plugin protocol (schema/event.v1.json, noticeMessage) with which an
// action asks the running service to restart with the new binary, e.g. after
// system.update replaced it. The core does not know which plugin sends it.
const noticeRestartService = "restart-service"

// Timings of a restart.
const (
	defaultRestartDelay = 500 * time.Millisecond
	// goAwayWait bounds how long the connections get to close before the exec.
	goAwayWait = 2 * time.Second
	goAwayPoll = 20 * time.Millisecond
)

// isRestartNotice reports whether a protocol message is the restart-service notice.
func isRestartNotice(msg map[string]any) bool {
	return msg["type"] == "notice" && msg["kind"] == noticeRestartService
}

// requestRestart records that a session asked for a restart and tells the clients. The
// restart itself waits in maybeRestart until no session is left.
func (s *Server) requestRestart(ref string) {
	if s.opts.Restart == nil {
		s.log.Warn("restart-service notice ignored: this service cannot restart itself", "action", ref)
		return
	}
	s.runMu.Lock()
	first := !s.restartWanted && !s.closing
	if first {
		s.restartWanted = true
	}
	s.runMu.Unlock()
	if first {
		s.log.Info("restart requested", "action", ref)
		s.bc.RestartRequested()
	}
}

// maybeRestart restarts the service when a restart is wanted and no session is queued
// or running; running sessions are never stopped for it, the last one to end triggers
// it. It returns when the restart failed or is not due.
func (s *Server) maybeRestart() {
	s.runMu.Lock()
	if !s.restartWanted || s.restarting || s.closing || s.live.count() > 0 {
		s.runMu.Unlock()
		return
	}
	// From here on spawnWork refuses sessions, so none can slip in before the exec.
	s.restarting = true
	s.runMu.Unlock()

	delay := s.opts.RestartDelay
	if delay <= 0 {
		delay = defaultRestartDelay
	}
	err := s.restartNow(delay)
	// Only a failed restart gets here: keep serving as before.
	s.runMu.Lock()
	s.restartWanted, s.restarting = false, false
	s.runMu.Unlock()
	s.hub.clearRestart()
	if err != nil {
		s.log.Error("restart failed; the service keeps running with the old version", "error", err)
	}
}

// restartNow gives the clients time to read the announcement, asks them to reconnect
// (close status 1012) and then calls Options.Restart. Listeners stay open until the
// exec closes them (close-on-exec), so a failed exec leaves a working service.
func (s *Server) restartNow(delay time.Duration) error {
	select {
	case <-time.After(delay):
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
	s.hub.goAway()
	deadline := time.Now().Add(goAwayWait)
	for s.hub.clientCount() > 0 && time.Now().Before(deadline) {
		select {
		case <-time.After(goAwayPoll):
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}
	s.log.Info("restarting")
	return s.opts.Restart()
}
