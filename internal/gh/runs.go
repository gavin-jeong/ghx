package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
)

// Workflow run logs are reached indirectly: a check's `link` URL carries the
// run (and sometimes job) id, which `gh run view --log` then needs.

// runURLRe extracts run and optional job ids from a check details URL, e.g.
// https://github.com/o/r/actions/runs/123456/job/789 → ("123456", "789").
var runURLRe = regexp.MustCompile(`/runs/(\d+)(?:/job/(\d+))?`)

// ParseRunURL pulls the run and job ids out of a check link.
func ParseRunURL(link string) (runID, jobID string, ok bool) {
	m := runURLRe.FindStringSubmatch(link)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// RunJob is one job within a workflow run.
type RunJob struct {
	DatabaseID int64  `json:"databaseId"`
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	Status     string `json:"status"`
}

// RunJobs lists the jobs of a run, used when the check link has no job id.
func (c *Client) RunJobs(ctx context.Context, runID string) ([]RunJob, error) {
	out, err := c.execRaw(ctx, "run", "view", runID, "--json", "jobs")
	if err != nil {
		return nil, err
	}
	var v struct {
		Jobs []RunJob `json:"jobs"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("decode run jobs: %w", err)
	}
	return v.Jobs, nil
}

// RunLogs fetches logs for a run, scoped to a job when jobID is non-empty.
// Log fetches can be much slower than other gh calls, so callers should use a
// longer timeout than the client default.
func (c *Client) RunLogs(ctx context.Context, runID, jobID string) (string, error) {
	args := []string{"run", "view", runID, "--log"}
	if jobID != "" {
		args = append(args, "--job", jobID)
	}
	out, err := c.execRaw(ctx, args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// FailedRunLogs fetches only the failed steps' logs — much smaller than --log
// for a big run, and usually what the reviewer actually wants.
func (c *Client) FailedRunLogs(ctx context.Context, runID string) (string, error) {
	out, err := c.execRaw(ctx, "run", "view", runID, "--log-failed")
	if err != nil {
		return "", err
	}
	return string(out), nil
}
