package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/pipeline"
	"github.com/savitharaghunathan/shuttle/internal/store"
	"github.com/spf13/cobra"
)

func newRunsCommand(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{Use: "runs", Short: "Inspect runs"}
	cmd.AddCommand(newRunsListCommand(out))
	return cmd
}

func newRunsListCommand(out io.Writer) *cobra.Command {
	var (
		agent, model, template, issue, statusCSV, since string
		limit                                           int
		asJSON                                          bool
		parent                                          string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List runs for this repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := openApp()
			if err != nil {
				return err
			}
			defer app.close()

			opts := store.ListRunsOpts{
				Limit:    limit,
				Agent:    agent,
				Template: template,
				IssueRef: issue,
			}
			if model == "unset" || model == "(default)" {
				opts.ModelUnset = true
			} else {
				opts.Model = model
			}
			if statusCSV != "" {
				opts.Statuses = parseStatuses(statusCSV)
			}
			if since != "" {
				t, err := parseSince(since)
				if err != nil {
					return err
				}
				opts.Since = t
			}
			if parent != "" {
				pid, err := app.store.ResolveRunID(parent)
				if err != nil {
					return err
				}
				opts.ParentID = pid
			}

			runs, err := app.store.ListRuns(opts)
			if err != nil {
				return err
			}
			if asJSON {
				return writeRunsJSON(out, app, runs)
			}
			return writeRunsTable(out, app, runs)
		},
	}
	f := cmd.Flags()
	f.StringVar(&agent, "agent", "", "filter by resolved agent")
	f.StringVar(&model, "model", "", "filter by model (use 'unset' for runs with no model)")
	f.StringVar(&template, "template", "", "filter by template id")
	f.StringVar(&issue, "issue", "", "filter by issue ref (org/repo#N)")
	f.StringVar(&statusCSV, "status", "", "comma-separated statuses (e.g. failed,running)")
	f.StringVar(&since, "since", "", "relative (7d, 24h) or date (2026-08-01)")
	f.StringVar(&parent, "parent", "", "filter by parent run id or prefix")
	f.IntVar(&limit, "limit", 20, "max rows")
	f.BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func parseStatuses(csv string) []store.RunStatus {
	var out []store.RunStatus
	for _, part := range strings.Split(csv, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			out = append(out, store.RunStatus(p))
		}
	}
	return out
}

func parseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if len(s) > 1 && s[len(s)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid --since %q (use 7d, 24h, or 2026-08-01)", s)
}

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

func runDuration(run store.Run) time.Duration {
	terminal := run.Status == store.RunStatusSucceeded ||
		run.Status == store.RunStatusFailed ||
		run.Status == store.RunStatusCancelled
	if terminal {
		return run.UpdatedAt.Sub(run.CreatedAt)
	}
	return time.Since(run.CreatedAt)
}

func runCurrentStage(app *appContext, run store.Run, cache map[string]*pipeline.Pipeline) string {
	pl, ok := cache[run.TemplateID]
	if !ok {
		loaded, err := loadPipelineTemplate(run.TemplateID)
		if err != nil {
			cache[run.TemplateID] = nil
		} else {
			pl = loaded
			cache[run.TemplateID] = loaded
		}
	}
	if pl == nil {
		return "unknown"
	}
	stages, err := app.store.GetStageRuns(run.ID)
	if err != nil {
		return "unknown"
	}
	return pipeline.CurrentStage(pl, stages)
}

func writeRunsTable(out io.Writer, app *appContext, runs []store.Run) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RUN\tPARENT\tSTATUS\tAGENT\tMODEL\tTEMPLATE\tISSUE\tSTARTED\tDURATION\tSTAGE")
	cache := map[string]*pipeline.Pipeline{}
	for _, run := range runs {
		agent := run.Agent
		if agent == "" {
			agent = "(unknown)"
		}
		model := run.Model
		if model == "" {
			model = "(default)"
		}
		parent := ""
		if run.ParentRunID != "" {
			parent = shortID(run.ParentRunID)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			shortID(run.ID), parent, run.Status, agent, model,
			run.TemplateID, run.IssueRef,
			run.CreatedAt.Local().Format("2006-01-02 15:04"),
			runDuration(run).Truncate(time.Second),
			runCurrentStage(app, run, cache),
		)
	}
	return tw.Flush()
}

func writeRunsJSON(out io.Writer, app *appContext, runs []store.Run) error {
	type runJSON struct {
		ID           string `json:"id"`
		ParentRunID  string `json:"parent_run_id,omitempty"`
		Status       string `json:"status"`
		Agent        string `json:"agent"`
		Model        string `json:"model"`
		TemplateID   string `json:"template_id"`
		IssueRef     string `json:"issue_ref,omitempty"`
		CreatedAt    string `json:"created_at"`
		UpdatedAt    string `json:"updated_at"`
		WallDuration int64  `json:"wall_duration"`
		CurrentStage string `json:"current_stage"`
	}
	cache := map[string]*pipeline.Pipeline{}
	list := make([]runJSON, 0, len(runs))
	for _, run := range runs {
		list = append(list, runJSON{
			ID: run.ID, ParentRunID: run.ParentRunID, Status: string(run.Status),
			Agent: run.Agent, Model: run.Model, TemplateID: run.TemplateID, IssueRef: run.IssueRef,
			CreatedAt: run.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: run.UpdatedAt.UTC().Format(time.RFC3339),
			WallDuration: int64(runDuration(run).Seconds()),
			CurrentStage: runCurrentStage(app, run, cache),
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(list)
}

