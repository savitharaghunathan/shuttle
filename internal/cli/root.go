package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/api"
	"github.com/savitharaghunathan/shuttle/internal/config"
	"github.com/savitharaghunathan/shuttle/internal/issue"
	"github.com/savitharaghunathan/shuttle/internal/pipeline"
	"github.com/savitharaghunathan/shuttle/internal/registry"
	"github.com/savitharaghunathan/shuttle/internal/store"
	"github.com/savitharaghunathan/shuttle/internal/templates"
	"github.com/savitharaghunathan/shuttle/internal/worker"
	"github.com/spf13/cobra"
)

func Execute() error {
	cmd := NewRootCommand(os.Stdout, os.Stderr)
	cmd.SetArgs(filterEmptyFlags(os.Args[1:]))
	return cmd.Execute()
}

func NewRootCommand(out, errOut io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "shuttle",
		Short:         "Run Shuttle pipelines",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errOut)

	root.AddCommand(
		newRunCommand(out),
		newImportCommand(out),
		newPRCommand(out, errOut),
		newReposCommand(out),
		newRunsCommand(out),
		newResumeCommand(out),
		newRetryCommand(out, errOut),
		newStatusCommand(out),
		newLogsCommand(out),
		newArtifactsCommand(out),
		newApproveCommand(out),
		newRejectCommand(out),
		newCancelCommand(out),
		newWorkersCommand(out),
		newServeCommand(out),
	)
	return root
}

func newRunCommand(out io.Writer) *cobra.Command {
	var templateID string
	var specPath string
	var issueURL string
	var agent string
	var model string
	var headless bool

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start a pipeline run",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedTemplate, err := resolveTemplateID(templateID, headless, issueURL != "", specPath != "")
			if err != nil {
				return err
			}

			pl, err := loadPipelineTemplate(resolvedTemplate)
			if err != nil {
				return err
			}

			cwd, specContent, issueRef, err := resolveRunInput(cmd.Context(), issueURL, specPath)
			if err != nil {
				return err
			}

			app, err := openAppInDir(cwd)
			if err != nil {
				return err
			}
			defer app.close()

			runID, err := app.engine.StartRun(cmd.Context(), pipeline.StartParams{
				TemplateID:  resolvedTemplate,
				Pipeline:    pl,
				CWD:         cwd,
				SpecPath:    specPath,
				SpecContent: specContent,
				Agent:       agent,
				Model:       model,
				IssueRef:    issueRef,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "run: %s\n", runID)
			fmt.Fprintf(out, "template: %s\n\n", resolvedTemplate)
			app.engine.Progress = out
			if err := app.engine.RunUntilBlockOrDone(cmd.Context(), runID); err != nil {
				return err
			}
			return printRun(out, app.store, runID, printRunOpts{hints: true})
		},
	}
	cmd.Flags().StringVarP(&templateID, "template", "t", "", "template id (pr-factory, pr-factory-headless, sdlc, sdlc-headless)")
	cmd.Flags().StringVar(&specPath, "spec", "", "path to the task spec")
	cmd.Flags().StringVar(&issueURL, "issue", "", "GitHub issue URL (requires imported repo)")
	cmd.Flags().StringVar(&agent, "agent", "", "agent runtime for the implement stage (sdlc read-only stages use readonly_worker)")
	cmd.Flags().StringVar(&model, "model", "", "model for every agent stage (sdlc: plan, design, implement, review)")
	cmd.Flags().BoolVar(&headless, "headless", false, "skip manual gates for pr-factory/sdlc (selects headless template)")
	return cmd
}

// resolveTemplateID applies template defaulting, the --headless swap, and input
// validation. It is kept free of I/O so the selection matrix can be unit tested.
func resolveTemplateID(templateID string, headless, hasIssue, hasSpec bool) (string, error) {
	if hasIssue && hasSpec {
		return "", fmt.Errorf("use either --issue or --spec, not both")
	}

	if templateID == "" {
		if headless || hasIssue {
			templateID = "pr-factory-headless"
		} else {
			templateID = "pr-factory"
		}
	}
	if headless {
		switch templateID {
		case "pr-factory":
			templateID = "pr-factory-headless"
		case "sdlc":
			templateID = "sdlc-headless"
		}
	}

	if !hasIssue && !hasSpec {
		switch templateID {
		case "pr-factory", "pr-factory-headless":
			return "", fmt.Errorf("--spec is required for pr-factory (or use --issue)")
		case "sdlc", "sdlc-headless":
			return "", fmt.Errorf("--spec is required for sdlc (or use --issue)")
		}
	}
	return templateID, nil
}

func newStatusCommand(out io.Writer) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status <run-id>",
		Short: "Show run status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := openApp()
			if err != nil {
				return err
			}
			defer app.close()
			runID, err := resolveRunArg(app, args[0])
			if err != nil {
				return err
			}
			return printRun(out, app.store, runID, printRunOpts{header: true, hints: true, asJSON: asJSON})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func newLogsCommand(out io.Writer) *cobra.Command {
	var stageFlag string
	var tail int
	cmd := &cobra.Command{
		Use:   "logs <run-id> [stage-id]",
		Short: "Show stage summaries and log files",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := openApp()
			if err != nil {
				return err
			}
			defer app.close()
			runID, err := resolveRunArg(app, args[0])
			if err != nil {
				return err
			}
			stages, err := app.store.GetStageRuns(runID)
			if err != nil {
				return err
			}
			filter := stageFlag
			if len(args) == 2 {
				filter = args[1]
			}
			wrote := false
			for _, sr := range stages {
				if filter != "" && sr.StageID != filter {
					continue
				}
				wrote = true
				fmt.Fprintf(out, "%s\t%s\n", sr.StageID, sr.Status)
				summary := ""
				if sr.Summary != nil {
					summary = *sr.Summary
				}
				if summary != "" {
					fmt.Fprintln(out, strings.TrimRight(tailLines(summary, tail), "\n"))
				}
				if sr.LogPath != nil && *sr.LogPath != "" {
					data, err := os.ReadFile(*sr.LogPath)
					if err != nil {
						return fmt.Errorf("read log %s: %w", *sr.LogPath, err)
					}
					// Stdout workers persist the same text as the stage summary;
					// printing it twice just doubles the noise.
					if strings.TrimSpace(string(data)) != strings.TrimSpace(summary) {
						fmt.Fprint(out, tailLines(string(data), tail))
					}
				}
			}
			if !wrote {
				if filter != "" {
					return fmt.Errorf("no logs found for stage %s in run %s", filter, runID)
				}
				return fmt.Errorf("no logs found for run %s", runID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&stageFlag, "stage", "", "limit output to one stage id")
	cmd.Flags().IntVar(&tail, "tail", 0, "show only the last N lines of each stage log (0 = all)")
	return cmd
}

// tailLines returns the last n lines of s, or all of s when n <= 0.
func tailLines(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n") + "\n"
}

func newApproveCommand(out io.Writer) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "approve <run-id> [stage-id]",
		Short: "Approve a manual gate",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := openApp()
			if err != nil {
				return err
			}
			defer app.close()
			runID, err := resolveRunArg(app, args[0])
			if err != nil {
				return err
			}
			stageID, err := gateStageID(app, runID, args[1:])
			if err != nil {
				return err
			}
			if err := app.engine.Approve(runID, stageID, message); err != nil {
				return err
			}
			fmt.Fprintf(out, "approved: %s %s\n\n", runID, stageID)
			app.engine.Progress = out
			if err := app.engine.RunUntilBlockOrDone(cmd.Context(), runID); err != nil {
				return err
			}
			return printRun(out, app.store, runID, printRunOpts{hints: true})
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "approved", "approval message")
	return cmd
}

func newRejectCommand(out io.Writer) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "reject <run-id> [stage-id]",
		Short: "Reject a manual gate",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := openApp()
			if err != nil {
				return err
			}
			defer app.close()
			runID, err := resolveRunArg(app, args[0])
			if err != nil {
				return err
			}
			stageID, err := gateStageID(app, runID, args[1:])
			if err != nil {
				return err
			}
			if err := app.engine.Reject(runID, stageID, message); err != nil {
				return err
			}
			fmt.Fprintf(out, "rejected: %s %s\n", runID, stageID)
			return nil
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "rejected", "rejection message")
	return cmd
}

func newCancelCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <run-id>",
		Short: "Cancel a run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := openApp()
			if err != nil {
				return err
			}
			defer app.close()
			runID, err := resolveRunArg(app, args[0])
			if err != nil {
				return err
			}
			if err := app.engine.Cancel(runID); err != nil {
				return err
			}
			fmt.Fprintf(out, "cancelled: %s\n", runID)
			return nil
		},
	}
}

func newWorkersCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "workers",
		Short: "List configured workers",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			reg := worker.NewRegistry(cfg)
			for _, name := range reg.Names() {
				fmt.Fprintln(out, name)
			}
			return nil
		},
	}
}

func newServeCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the Shuttle server",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := openApp()
			if err != nil {
				return err
			}
			defer app.close()

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			srv := &http.Server{
				Addr: "127.0.0.1:8080",
				Handler: api.NewHandler(app.engine, api.Options{
					CWD:         cwd,
					PipelineIDs: templates.IDs(),
				}),
			}
			fmt.Fprintln(out, "serving on http://127.0.0.1:8080")
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
}

type appContext struct {
	cfg    *config.Config
	store  *store.Store
	engine *pipeline.Engine
}

func openApp() (*appContext, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return openAppInDir(cwd)
}

func openAppInDir(cwd string) (*appContext, error) {
	cfg, err := loadConfigInDir(cwd)
	if err != nil {
		return nil, err
	}
	workRoot := cfg.WorkRoot
	if !filepath.IsAbs(workRoot) {
		workRoot = filepath.Join(cwd, workRoot)
	}
	cfg.WorkRoot = workRoot
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create work root %s: %w", workRoot, err)
	}
	dbPath := filepath.Join(workRoot, "shuttle.db")
	s, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	reg := worker.NewRegistry(cfg)
	eng := pipeline.NewEngine(s, reg, cfg)
	eng.TemplateLoader = loadPipelineTemplate
	return &appContext{
		cfg:    cfg,
		store:  s,
		engine: eng,
	}, nil
}

func (a *appContext) close() {
	_ = a.store.Close()
}

func loadConfig() (*config.Config, error) {
	return loadConfigInDir("")
}

func loadConfigInDir(cwd string) (*config.Config, error) {
	configPath := "shuttle.yaml"
	if cwd != "" {
		configPath = filepath.Join(cwd, "shuttle.yaml")
	}
	cfg, err := config.Load(configPath)
	if err == nil {
		return cfg, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return config.LoadDefaults(), nil
	}
	return nil, err
}

func resolveRunInput(ctx context.Context, issueURL, specPath string) (cwd, specContent, issueRef string, err error) {
	if issueURL == "" {
		cwd, err = os.Getwd()
		return cwd, "", "", err
	}

	owner, name, number, err := issue.ParseIssueURL(issueURL)
	if err != nil {
		return "", "", "", err
	}

	reg, err := openRegistry()
	if err != nil {
		return "", "", "", err
	}
	defer reg.Close()

	entry, err := reg.Get(owner, name)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return "", "", "", fmt.Errorf("repo %s/%s not imported; run: shuttle import repo https://github.com/%s/%s", owner, name, owner, name)
		}
		return "", "", "", err
	}

	specContent, err = issue.FetchSpec(ctx, owner, name, number)
	if err != nil {
		return "", "", "", err
	}
	return entry.Path, specContent, fmt.Sprintf("%s/%s#%d", owner, name, number), nil
}

func resolveRunArg(app *appContext, arg string) (string, error) {
	if arg == "" {
		return "", fmt.Errorf("empty run id")
	}
	return app.store.ResolveRunID(arg)
}

func loadPipelineTemplate(templateID string) (*pipeline.Pipeline, error) {
	pl, embeddedErr := templates.Get(templateID)
	if embeddedErr == nil {
		return pl, nil
	}

	for _, candidate := range pipelineCandidates(templateID) {
		data, err := os.ReadFile(candidate)
		if err == nil {
			pl, err := pipeline.LoadBytes(data)
			if err != nil {
				return nil, fmt.Errorf("load pipeline %q: %w", candidate, err)
			}
			return pl, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read pipeline %q: %w", candidate, err)
		}
	}

	return nil, embeddedErr
}

func pipelineCandidates(templateID string) []string {
	ext := filepath.Ext(templateID)
	isYAML := ext == ".yaml" || ext == ".yml"
	hasPath := strings.Contains(templateID, string(filepath.Separator))

	var candidates []string
	if isYAML || hasPath || filepath.IsAbs(templateID) {
		candidates = append(candidates, templateID)
	}
	if !hasPath && !filepath.IsAbs(templateID) {
		candidates = append(candidates, filepath.Join("pipelines", templateID+".yaml"))
		candidates = append(candidates, filepath.Join("pipelines", templateID+".yml"))
	}
	return candidates
}

// gateStageID resolves the stage an approve/reject targets. With no explicit
// stage argument it infers the single stage waiting for approval.
func gateStageID(app *appContext, runID string, args []string) (string, error) {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		return args[0], nil
	}
	stages, err := app.store.GetStageRuns(runID)
	if err != nil {
		return "", err
	}
	var waiting []string
	for _, sr := range stages {
		if sr.Status == store.StageStatusWaitingApproval {
			waiting = append(waiting, sr.StageID)
		}
	}
	switch len(waiting) {
	case 1:
		return waiting[0], nil
	case 0:
		return "", fmt.Errorf("run %s has no stage waiting for approval", runID)
	default:
		return "", fmt.Errorf("run %s has %d stages waiting for approval (%s); name one", runID, len(waiting), strings.Join(waiting, ", "))
	}
}

type printRunOpts struct {
	header bool // print the run/template/metadata block
	hints  bool // print gate hints when the run is blocked
	asJSON bool
}

func printRun(out io.Writer, s *store.Store, runID string, opts printRunOpts) error {
	run, err := s.GetRun(runID)
	if err != nil {
		return err
	}
	stages, err := s.GetStageRuns(runID)
	if err != nil {
		return err
	}
	stages = orderStages(run.TemplateID, stages)
	if opts.asJSON {
		return writeRunDetailJSON(out, s, run, stages)
	}

	if opts.header {
		fmt.Fprintf(out, "run: %s\n", run.ID)
		fmt.Fprintf(out, "template: %s\n", run.TemplateID)
		printRunMetadata(out, s, run)
	} else {
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "status: %s\n", run.Status)
	for _, sr := range stages {
		fmt.Fprintf(out, "stage: %s\tstatus: %s\tworker: %s\tattempt: %d\n", sr.StageID, sr.Status, sr.Worker, sr.Attempt)
	}
	if opts.hints && run.Status == store.RunStatusBlocked {
		printGateHints(out, run, stages)
	}
	return nil
}

func printRunMetadata(out io.Writer, s *store.Store, run store.Run) {
	if mode := outputsMode(run.TemplateID); mode != "" {
		fmt.Fprintf(out, "outputs: %s\n", mode)
	}
	if run.IssueRef != "" {
		fmt.Fprintf(out, "issue: %s\n", run.IssueRef)
	}
	if branch := runBranch(s, run); branch != "" {
		fmt.Fprintf(out, "branch: %s\n", branch)
	}
	agent := run.Agent
	if agent == "" {
		agent = "(unknown)"
	}
	model := run.Model
	if model == "" {
		model = "(default)"
	}
	fmt.Fprintf(out, "agent: %s\tmodel: %s\n", agent, model)
	fmt.Fprintf(out, "run dir: %s\n", run.RunDir)
}

func printGateHints(out io.Writer, run store.Run, stages []store.StageRun) {
	for _, sr := range stages {
		if sr.Status != store.StageStatusWaitingApproval {
			continue
		}
		fmt.Fprintf(out, "approval: shuttle approve %s %s --message \"approved\"\n", run.ID, sr.StageID)
		fmt.Fprintf(out, "rejection: shuttle reject %s %s --message \"reason\"\n", run.ID, sr.StageID)
		if hasArtifacts(run.RunDir) {
			fmt.Fprintf(out, "artifacts: shuttle artifacts %s\n", run.ID)
		}
	}
}

// orderStages sorts stage rows into the order the template declares them, so a
// multi-stage run reads top to bottom. Stages missing from the template (or any
// run whose template no longer loads) keep their stored order.
func orderStages(templateID string, stages []store.StageRun) []store.StageRun {
	pl, err := loadPipelineTemplate(templateID)
	if err != nil {
		return stages
	}
	position := make(map[string]int, len(pl.Stages))
	for i, stage := range pl.Stages {
		position[stage.ID] = i
	}
	ordered := append([]store.StageRun(nil), stages...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, leftKnown := position[ordered[i].StageID]
		right, rightKnown := position[ordered[j].StageID]
		if leftKnown && rightKnown {
			return left < right
		}
		return leftKnown && !rightKnown
	})
	return ordered
}

// outputsMode reports how declared stage outputs are enforced: "strict" fails a
// stage that produced no artifact, "bridge" falls back to worker stdout. It
// returns "" for templates that declare no outputs.
func outputsMode(templateID string) string {
	pl, err := loadPipelineTemplate(templateID)
	if err != nil {
		return ""
	}
	declaresOutputs := false
	for _, stage := range pl.Stages {
		if len(stage.Outputs) > 0 {
			declaresOutputs = true
			break
		}
	}
	if !declaresOutputs {
		return ""
	}
	if pipeline.IsStrictOutputTemplate(templateID) {
		return "strict"
	}
	return "bridge"
}

func runBranch(s *store.Store, run store.Run) string {
	branch, err := issue.ResolveBranch(run.IssueRef, run.ID, run.ParentRunID, func(id string) (string, string, string, error) {
		parent, err := s.GetRun(id)
		if err != nil {
			return "", "", "", err
		}
		return parent.IssueRef, parent.ID, parent.ParentRunID, nil
	})
	if err != nil {
		return ""
	}
	return branch
}

func writeRunDetailJSON(out io.Writer, s *store.Store, run store.Run, stages []store.StageRun) error {
	type stageJSON struct {
		ID      string  `json:"id"`
		Worker  string  `json:"worker"`
		Status  string  `json:"status"`
		Attempt int     `json:"attempt"`
		Retries int     `json:"retries"`
		Summary *string `json:"summary,omitempty"`
		LogPath *string `json:"log_path,omitempty"`
	}
	type runJSON struct {
		ID           string      `json:"id"`
		ParentRunID  string      `json:"parent_run_id,omitempty"`
		TemplateID   string      `json:"template_id"`
		Status       string      `json:"status"`
		Agent        string      `json:"agent"`
		Model        string      `json:"model"`
		IssueRef     string      `json:"issue_ref,omitempty"`
		Branch       string      `json:"branch,omitempty"`
		OutputsMode  string      `json:"outputs_mode,omitempty"`
		CWD          string      `json:"cwd"`
		RunDir       string      `json:"run_dir"`
		CreatedAt    string      `json:"created_at"`
		UpdatedAt    string      `json:"updated_at"`
		WallDuration int64       `json:"wall_duration"`
		CurrentStage string      `json:"current_stage"`
		Stages       []stageJSON `json:"stages"`
	}

	currentStage := ""
	if pl, err := loadPipelineTemplate(run.TemplateID); err == nil {
		currentStage = pipeline.CurrentStage(pl, stages)
	}
	detail := runJSON{
		ID:           run.ID,
		ParentRunID:  run.ParentRunID,
		TemplateID:   run.TemplateID,
		Status:       string(run.Status),
		Agent:        run.Agent,
		Model:        run.Model,
		IssueRef:     run.IssueRef,
		Branch:       runBranch(s, run),
		OutputsMode:  outputsMode(run.TemplateID),
		CWD:          run.CWD,
		RunDir:       run.RunDir,
		CreatedAt:    run.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    run.UpdatedAt.UTC().Format(time.RFC3339),
		WallDuration: int64(runDuration(run).Seconds()),
		CurrentStage: currentStage,
		Stages:       make([]stageJSON, 0, len(stages)),
	}
	for _, sr := range stages {
		detail.Stages = append(detail.Stages, stageJSON{
			ID:      sr.StageID,
			Worker:  sr.Worker,
			Status:  string(sr.Status),
			Attempt: sr.Attempt,
			Retries: sr.Retries,
			Summary: sr.Summary,
			LogPath: sr.LogPath,
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(detail)
}

func init() {
	cobra.EnableCommandSorting = false
}
