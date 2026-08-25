package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/savitharaghunathan/shuttle/internal/config"
	"github.com/savitharaghunathan/shuttle/internal/pipeline"
	"github.com/savitharaghunathan/shuttle/internal/store"
	"github.com/savitharaghunathan/shuttle/internal/templates"
	"github.com/savitharaghunathan/shuttle/internal/worker"
	"github.com/spf13/cobra"
)

func Execute() error {
	return NewRootCommand(os.Stdout, os.Stderr).Execute()
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

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start a pipeline run",
		RunE: func(cmd *cobra.Command, args []string) error {
			if templateID == "" {
				return fmt.Errorf("--template is required")
			}
			if templateID == "pr-factory" && specPath == "" {
				return fmt.Errorf("--spec is required for pr-factory")
			}

			pl, err := templates.Get(templateID)
			if err != nil {
				return err
			}
			app, err := openApp()
			if err != nil {
				return err
			}
			defer app.close()

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			runID, err := app.engine.StartRun(cmd.Context(), pipeline.StartParams{
				TemplateID: templateID,
				Pipeline:   pl,
				CWD:        cwd,
				SpecPath:   specPath,
			})
			if err != nil {
				return err
			}
			if err := app.engine.RunUntilBlockOrDone(cmd.Context(), runID); err != nil {
				return err
			}
			return printRun(out, app.store, runID, true)
		},
	}
	cmd.Flags().StringVarP(&templateID, "template", "t", "", "template id (pr-factory or sdlc)")
	cmd.Flags().StringVar(&specPath, "spec", "", "path to the task spec")
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
			return printRun(out, app.store, args[0], false)
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

			stages, err := app.store.GetStageRuns(args[0])
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
				return fmt.Errorf("no logs found for run %s", args[0])
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
			if err := app.engine.Approve(args[0], args[1], message); err != nil {
				return err
			}
			fmt.Fprintf(out, "approved: %s %s\n", args[0], args[1])
			if err := app.engine.RunUntilBlockOrDone(cmd.Context(), args[0]); err != nil {
				return err
			}
			return printRun(out, app.store, args[0], true)
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
			if err := app.engine.Reject(args[0], args[1], message); err != nil {
				return err
			}
			fmt.Fprintf(out, "rejected: %s %s\n", args[0], args[1])
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
			if err := app.engine.Cancel(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(out, "cancelled: %s\n", args[0])
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
			fmt.Fprintln(out, "serve is not implemented until Task 9")
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
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.WorkRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create work root %s: %w", cfg.WorkRoot, err)
	}
	dbPath := filepath.Join(cfg.WorkRoot, "shuttle.db")
	s, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	reg := worker.NewRegistry(cfg)
	eng := pipeline.NewEngine(s, reg, cfg)
	eng.TemplateLoader = templates.Get
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
	const configPath = "shuttle.yaml"
	cfg, err := config.Load(configPath)
	if err == nil {
		return cfg, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return config.LoadDefaults(), nil
	}
	return nil, err
}

func printRun(out io.Writer, s *store.Store, runID string, includeHints bool) error {
	run, err := s.GetRun(runID)
	if err != nil {
		return err
	}
	stages, err := s.GetStageRuns(runID)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "run: %s\n", run.ID)
	fmt.Fprintf(out, "template: %s\n", run.TemplateID)
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
