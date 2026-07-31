package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/keyolk/ghx/internal/pr"
)

// Actions that operate on a PR without opening it. These exist so the list view
// can act on a row directly; each takes the number explicitly rather than
// relying on a checkout, because ghx runs outside any repository.

// Close closes a PR. comment, when non-empty, is posted first so the close has
// a stated reason — a silent close leaves the author guessing.
func (c *Client) Close(ctx context.Context, number int, comment string) error {
	args := []string{"pr", "close", fmt.Sprintf("%d", number)}
	if comment != "" {
		args = append(args, "--comment", comment)
	}
	_, err := c.exec(ctx, args...)
	return err
}

// Reopen reopens a closed PR.
func (c *Client) Reopen(ctx context.Context, number int) error {
	_, err := c.exec(ctx, "pr", "reopen", fmt.Sprintf("%d", number))
	return err
}

// AddLabels attaches labels to a PR. gh takes them as a repeated flag.
func (c *Client) AddLabels(ctx context.Context, number int, labels []string) error {
	if len(labels) == 0 {
		return fmt.Errorf("no labels given")
	}
	args := []string{"pr", "edit", fmt.Sprintf("%d", number)}
	for _, l := range labels {
		args = append(args, "--add-label", l)
	}
	_, err := c.exec(ctx, args...)
	return err
}

// RemoveLabels detaches labels from a PR.
func (c *Client) RemoveLabels(ctx context.Context, number int, labels []string) error {
	if len(labels) == 0 {
		return fmt.Errorf("no labels given")
	}
	args := []string{"pr", "edit", fmt.Sprintf("%d", number)}
	for _, l := range labels {
		args = append(args, "--remove-label", l)
	}
	_, err := c.exec(ctx, args...)
	return err
}

// RepoLabel is a label available in the repository.
type RepoLabel struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

// RepoLabels lists the labels defined in the repo, for the label picker.
func (c *Client) RepoLabels(ctx context.Context) ([]RepoLabel, error) {
	out, err := c.exec(ctx, "label", "list", "--limit", "200",
		"--json", "name,description,color")
	if err != nil {
		return nil, err
	}
	var labels []RepoLabel
	if err := json.Unmarshal(out, &labels); err != nil {
		return nil, fmt.Errorf("decode labels: %w", err)
	}
	return labels, nil
}

// PRLabels returns the labels currently on a PR, so a picker can show what is
// already applied instead of re-adding it.
func (c *Client) PRLabels(ctx context.Context, number int) ([]string, error) {
	out, err := c.exec(ctx, "pr", "view", fmt.Sprintf("%d", number), "--json", "labels")
	if err != nil {
		return nil, err
	}
	var v struct {
		Labels []pr.Label `json:"labels"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("decode pr labels: %w", err)
	}
	names := make([]string, 0, len(v.Labels))
	for _, l := range v.Labels {
		names = append(names, l.Name)
	}
	return names, nil
}

// PRPermalink returns the web URL for a PR, used by the browser action when the
// list row did not carry one.
func (c *Client) PRPermalink(ctx context.Context, number int) (string, error) {
	out, err := c.exec(ctx, "pr", "view", fmt.Sprintf("%d", number), "--json", "url")
	if err != nil {
		return "", err
	}
	var v struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return "", fmt.Errorf("decode pr url: %w", err)
	}
	if v.URL == "" {
		return "", fmt.Errorf("pull request %d has no url", number)
	}
	return strings.TrimSpace(v.URL), nil
}
