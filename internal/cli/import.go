package cli

import (
	"context"
	"fmt"
	"io"
	"os"

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
	cmd := &cobra.Command{
		Use:   "repo <github-url>",
		Short: "Clone and register a GitHub repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := os.MkdirAll(home.Dir(), 0o755); err != nil {
				return err
			}
			reg, err := registry.Open(home.RegistryDB())
			if err != nil {
				return err
			}
			defer reg.Close()

			entry, err := repo.Import(cmd.Context(), args[0], reg, repo.ImportOpts{})
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "imported: %s/%s\n", entry.Owner, entry.Name)
			fmt.Fprintf(out, "path: %s\n", entry.Path)
			return nil
		},
	}
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

func importRepo(ctx context.Context, rawURL string) (registry.Repo, error) {
	reg, err := openRegistry()
	if err != nil {
		return registry.Repo{}, err
	}
	defer reg.Close()
	return repo.Import(ctx, rawURL, reg, repo.ImportOpts{})
}
