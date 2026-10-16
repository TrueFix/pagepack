package web

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/valkey-io/valkey-go"
)

// Registered before the worker starts, so even a pending job can be canceled.
// mu serializes cancellation against the final successful state commit.
type jobControl struct {
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	mu        sync.Mutex
	requested bool
	finalized bool
}

func (s *Server) registerJob(id string) *jobControl {
	ctx, cancel := context.WithTimeout(s.ctx, s.config.JobTimeout)
	c := &jobControl{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if s.active == nil {
		s.active = make(map[string]*jobControl)
	}
	s.active[id] = c
	return c
}

// POST /api/v1/jobs/{id}/cancel. 200 means the worker has stopped and cleaned up;
// 202 means shutdown/cleanup is still underway. Poll the normal job endpoint.
func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if _, err := uuid.Parse(id); err != nil {
		failure(w, 400, errInvalidID, "Invalid request ID")
		return
	}
	job, err := s.state.Get(r.Context(), id)
	if valkey.IsValkeyNil(err) {
		failure(w, 404, errNotFound, "Job not found or expired")
		return
	}
	if err != nil {
		failure(w, 503, errUnavailable, "Job state is unavailable")
		return
	}
	if job.Status == StatusCanceled {
		respond(w, 200, job.View(s.config.MaxWebsiteBytes, nil, time.Now().UTC()))
		return
	}
	if job.Status.Finished() {
		failure(w, 409, "job_finished", "The job has already finished")
		return
	}
	s.activeMu.Lock()
	c := s.active[id]
	s.activeMu.Unlock()
	if c == nil {
		failure(w, 409, "job_not_running", "This process is not running that job")
		return
	}
	c.mu.Lock()
	if c.finalized {
		c.mu.Unlock()
		failure(w, 409, "job_finished", "The job has already finished")
		return
	}
	first := !c.requested
	c.requested = true
	c.cancel()
	c.mu.Unlock()
	if first {
		s.logger().Info("cancellation requested", "job", shortID(id))
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-c.done:
		job, err = s.state.Get(r.Context(), id)
		if err != nil || !job.Status.Finished() {
			failure(w, 503, errUnavailable, "Worker stopped, but final job state could not be read or saved")
			return
		}
		respond(w, 200, job.View(s.config.MaxWebsiteBytes, nil, time.Now().UTC()))
	case <-timer.C:
		w.Header().Set("Location", "/api/v1/jobs/"+id)
		respond(w, 202, map[string]any{"request_id": id, "status": "canceling", "summary": "Cancellation requested; waiting for the worker and cleanup", "links": map[string]string{"self": "/api/v1/jobs/" + id}})
	case <-r.Context().Done():
		// Cancellation continues even if the caller disconnects.
	}
}
