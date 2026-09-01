package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
			if templateID == "" {
				if headless || issueURL != "" {
					templateID = "pr-factory-headless"
				} else {
					templateID = "pr-factory"
				}
			}
			if headless && templateID == "pr-factory" {
				templateID = "pr-factory-headless"
			}

			if issueURL == "" && templateID == "pr-factory" && specPath == "" {
				return fmt.Errorf("--spec is required for pr-factory (or use --issue)")
			}
			if issueURL != "" && specPath != "" {
				return fmt.Errorf("use either --issue or --spec, not both")
			}

			pl, err := loadPipelineTemplate(templateID)
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
				TemplateID:  templateID,
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
			fmt.Fprintf(out, "template: %s\n\n", templateID)
			app.engine.Progress = out
			if err := app.engine.RunUntilBlockOrDone(cmd.Context(), runID); err != nil {
				return err
			}
			return printRun(out, app.store, runID, true, true)
		},
	}
	cmd.Flags().StringVarP(&templateID, "template", "t", "", "template id (pr-factory, pr-factory-headless, sdlc)")
	cmd.Flags().StringVar(&specPath, "spec", "", "path to the task spec")
	cmd.Flags().StringVar(&issueURL, "issue", "", "GitHub issue URL (requires imported repo)")
	cmd.Flags().StringVar(&agent, "agent", "", "agent runtime for implement stages (built-ins or any workers: name in shuttle.yaml)")
	cmd.Flags().StringVar(&model, "model", "", "model passed to agent runtime ({{.Model}} in preset args)")
	cmd.Flags().BoolVar(&headless, "headless", false, "run without manual approval gates")
	return cmd
}

func newStatusCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
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
			return printRun(out, app.store, runID, false)
		},
	}
}

func newLogsCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
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
			filter := ""
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
				if sr.Summary != nil && *sr.Summary != "" {
					fmt.Fprintln(out, *sr.Summary)
				}
				if sr.LogPath != nil && *sr.LogPath != "" {
					data, err := os.ReadFile(*sr.LogPath)
					if err != nil {
						return fmt.Errorf("read log %s: %w", *sr.LogPath, err)
					}
					fmt.Fprint(out, string(data))
				}
			}
			if !wrote {
				return fmt.Errorf("no logs found for run %s", runID)
			}
			return nil
		},
	}
}

func newApproveCommand(out io.Writer) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "approve <run-id> <stage-id>",
		Short: "Approve a manual gate",
		Args:  cobra.ExactArgs(2),
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
			if err := app.engine.Approve(runID, args[1], message); err != nil {
				return err
			}
			fmt.Fprintf(out, "approved: %s %s\n\n", runID, args[1])
			app.engine.Progress = out
			if err := app.engine.RunUntilBlockOrDone(cmd.Context(), runID); err != nil {
				return err
			}
			return printRun(out, app.store, args[0], true, true)
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "approved", "approval message")
	return cmd
}

func newRejectCommand(out io.Writer) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "reject <run-id> <stage-id>",
		Short: "Reject a manual gate",
		Args:  cobra.ExactArgs(2),
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
			if err := app.engine.Reject(runID, args[1], message); err != nil {
				return err
			}
			fmt.Fprintf(out, "rejected: %s %s\n", runID, args[1])
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

func printRun(out io.Writer, s *store.Store, runID string, includeHints bool, skipHeader ...bool) error {
	run, err := s.GetRun(runID)
	if err != nil {
		return err
	}
	stages, err := s.GetStageRuns(runID)
	if err != nil {
		return err
	}

	if len(skipHeader) == 0 || !skipHeader[0] {
		fmt.Fprintf(out, "run: %s\n", run.ID)
		fmt.Fprintf(out, "template: %s\n", run.TemplateID)
	} else {
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "status: %s\n", run.Status)
	for _, sr := range stages {
		fmt.Fprintf(out, "stage: %s\tstatus: %s\tworker: %s\tattempt: %d\n", sr.StageID, sr.Status, sr.Worker, sr.Attempt)
	}
	if includeHints && run.Status == store.RunStatusBlocked {
		for _, sr := range stages {
			if sr.Status == store.StageStatusWaitingApproval {
				fmt.Fprintf(out, "approval: shuttle approve %s %s --message \"approved\"\n", run.ID, sr.StageID)
				fmt.Fprintf(out, "rejection: shuttle reject %s %s --message \"reason\"\n", run.ID, sr.StageID)
			}
		}
	}
	return nil
}

func init() {
	cobra.EnableCommandSorting = false
}
