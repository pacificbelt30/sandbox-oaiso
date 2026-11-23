//go:build linux

package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
)

func TestJobRetrySpecIsScopedAndRequiresRetainedFailure(t *testing.T) {
	job := execution{ID: "job", Container: "worker", Detached: true, Started: true, ExitCode: 23, FinishedAt: time.Now(),
		Config: container.ExecOptions{Cmd: []string{"printf", "%s", "literal []"}, WorkingDir: "/workspace/subdir"}}
	for _, test := range []struct {
		name   string
		worker string
		edit   func(*execution)
		status int
	}{
		{"failed", "worker", func(*execution) {}, http.StatusOK},
		{"different worker", "other", func(*execution) {}, http.StatusNotFound},
		{"streaming execution", "worker", func(e *execution) { e.Detached = false }, http.StatusNotFound},
		{"running", "worker", func(e *execution) { e.Running = true }, http.StatusConflict},
		{"unconfirmed exit", "worker", func(e *execution) { e.FinishedAt = time.Time{} }, http.StatusConflict},
		{"success", "worker", func(e *execution) { e.ExitCode = 0 }, http.StatusConflict},
		{"custom environment", "worker", func(e *execution) { e.Config.Env = []string{"FIXTURE=private"} }, http.StatusConflict},
		{"inherited directory", "worker", func(e *execution) { e.Config.WorkingDir = "" }, http.StatusConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := job
			test.edit(&record)
			node := Backend{execs: map[string]*execution{"job": &record}}
			response := httptest.NewRecorder()
			node.serveHTTP(response, httptest.NewRequest(http.MethodGet, "/containers/"+test.worker+"/jobs/job/retry-spec", nil))
			if response.Code != test.status {
				t.Fatalf("status %d, want %d", response.Code, test.status)
			}
			if test.status != http.StatusOK {
				return
			}
			var spec struct {
				Worker     string
				ID         string
				Argv       []string
				WorkingDir string `json:"working_dir"`
				ExitCode   int    `json:"exit_code"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &spec); err != nil ||
				spec.Worker != job.Container || spec.ID != job.ID || spec.ExitCode != job.ExitCode ||
				spec.WorkingDir != job.Config.WorkingDir || !slices.Equal(spec.Argv, job.Config.Cmd) {
				t.Fatal(spec, err)
			}
		})
	}
}
