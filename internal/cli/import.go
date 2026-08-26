package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/savitharaghunathan/shuttle/internal/home"
	"github.com/savitharaghunathan/shuttle/internal/registry"
	"github.com/savitharaghunathan/shuttle/internal/repo"
	"github.com/spf13/cobra"
)

func newImportCommand(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import external resources into Shuttle",
	}
	cmd.AddCommand(newImportRepoCommand(out))
	return cmd
}

func newImportRepoCommand(out io.Writer) *cobra.Command {
	var pathFlag string
	cmd := &cobra.Command{
		Use:   "repo [github-url|.]",
		Short: "Clone or register a GitHub repository",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rawURL := ""
			if len(args) == 1 {
				rawURL = strings.TrimSpace(args[0])
			}

			localPath := strings.TrimSpace(pathFlag)
			if rawURL == "." {
				if localPath != "" {
					return fmt.Errorf("use either --path or ., not both")
				}
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				localPath = cwd
				rawURL = ""
			}

			if localPath == "" && rawURL == "" {
				return fmt.Errorf("github url required (or use --path / .)")
			}

			reg, err := openRegistry()
			if err != nil {
				return err
			}
			defer reg.Close()

			opts := repo.ImportOpts{}
			if localPath != "" {
				abs, err := filepath.Abs(localPath)
				if err != nil {
					return err
				}
				opts.LocalPath = abs
			}

			entry, err := repo.Import(cmd.Context(), rawURL, reg, opts)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "imported: %s/%s\n", entry.Owner, entry.Name)
			fmt.Fprintf(out, "path: %s\n", entry.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&pathFlag, "path", "", "existing local checkout to register (no clone)")
	return cmd
}

func newReposCommand(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repos",
		Short: "Manage imported repositories",
	}
	cmd.AddCommand(newReposListCommand(out))
	return cmd
}

func newReposListCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List imported repositories",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := openRegistry()
			if err != nil {
				return err
			}
			defer reg.Close()

			repos, err := reg.List()
			if err != nil {
				return err
			}
			if len(repos) == 0 {
				fmt.Fprintln(out, "no imported repos")
				return nil
			}
			for _, r := range repos {
				fmt.Fprintf(out, "%s/%s\t%s\t%s\n", r.Owner, r.Name, r.URL, r.Path)
			}
			return nil
		},
	}
}

func openRegistry() (*registry.Registry, error) {
	if err := os.MkdirAll(home.Dir(), 0o755); err != nil {
		return nil, err
	}
	return registry.Open(home.RegistryDB())
}
