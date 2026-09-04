package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// artifact is a stage deliverable persisted under <run-dir>/artifacts.
type artifact struct {
	Name string // "spec" or "<stage-id>/<file>"
	Path string
	Size int64
}

func newArtifactsCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "artifacts <run-id> [name]",
		Short: "List or print stage artifacts for a run",
		Long: "List the deliverables a run persisted under its artifacts directory, " +
			"or print one by name (a stage id such as 'plan', or 'plan/summary').",
		Args: cobra.RangeArgs(1, 2),
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
			run, err := app.store.GetRun(runID)
			if err != nil {
				return err
			}

			entries, err := listArtifacts(run.RunDir)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				return fmt.Errorf("no artifacts found for run %s (looked in %s)", runID, filepath.Join(run.RunDir, "artifacts"))
			}
			if len(args) == 1 {
				return writeArtifactTable(out, entries)
			}
			return printArtifact(out, entries, args[1])
		},
	}
}

// listArtifacts returns the artifact files under runDir, one entry per file.
// Stage deliverables live in per-stage directories; the frozen spec is a file
// at the top level.
func listArtifacts(runDir string) ([]artifact, error) {
	root := filepath.Join(runDir, "artifacts")
	top, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read artifacts dir %s: %w", root, err)
	}

	var entries []artifact
	for _, item := range top {
		path := filepath.Join(root, item.Name())
		if !item.IsDir() {
			if info, err := item.Info(); err == nil {
				entries = append(entries, artifact{Name: item.Name(), Path: path, Size: info.Size()})
			}
			continue
		}
		files, err := os.ReadDir(path)
		if err != nil {
			return nil, fmt.Errorf("read artifacts dir %s: %w", path, err)
		}
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			info, err := file.Info()
			if err != nil {
				continue
			}
			entries = append(entries, artifact{
				Name: item.Name() + "/" + file.Name(),
				Path: filepath.Join(path, file.Name()),
				Size: info.Size(),
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func hasArtifacts(runDir string) bool {
	entries, err := listArtifacts(runDir)
	return err == nil && len(entries) > 0
}

func writeArtifactTable(out io.Writer, entries []artifact) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ARTIFACT\tBYTES\tPATH")
	for _, entry := range entries {
		fmt.Fprintf(tw, "%s\t%d\t%s\n", entry.Name, entry.Size, entry.Path)
	}
	return tw.Flush()
}

// printArtifact writes the contents of the artifact matching name. A stage id
// matches every file that stage produced.
func printArtifact(out io.Writer, entries []artifact, name string) error {
	name = strings.Trim(strings.TrimSpace(name), "/")
	var matches []artifact
	for _, entry := range entries {
		if entry.Name == name || strings.HasPrefix(entry.Name, name+"/") {
			matches = append(matches, entry)
		}
	}
	if len(matches) == 0 {
		available := make([]string, 0, len(entries))
		for _, entry := range entries {
			available = append(available, entry.Name)
		}
		return fmt.Errorf("no artifact %q; available: %s", name, strings.Join(available, ", "))
	}

	for _, entry := range matches {
		data, err := os.ReadFile(entry.Path)
		if err != nil {
			return fmt.Errorf("read artifact %s: %w", entry.Path, err)
		}
		if len(matches) > 1 {
			fmt.Fprintf(out, "== %s ==\n", entry.Name)
		}
		fmt.Fprint(out, string(data))
		if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
			fmt.Fprintln(out)
		}
	}
	return nil
}
