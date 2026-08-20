package work

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newListCommand(configDirectory string, output io.Writer) *cobra.Command {
	var spaceID string
	command := &cobra.Command{
		Use:   "list",
		Short: "List Work in a Space",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			client, selectedSpaceID, err := connect(command.Context(), configDirectory, spaceID)
			if err != nil {
				return err
			}
			page, err := client.ListWorks(command.Context(), selectedSpaceID, "")
			if err != nil {
				return err
			}
			if len(page.Works) == 0 {
				_, err = fmt.Fprintln(output, "No Work.")
				return err
			}
			for _, item := range page.Works {
				if _, err := fmt.Fprintf(output, "%s\t%s\t%s\n", item.WorkID, item.Lifecycle, item.Goal); err != nil {
					return err
				}
			}
			if page.HasEarlier {
				_, err = fmt.Fprintln(output, "Earlier Work is not shown.")
				return err
			}
			return nil
		},
	}
	command.Flags().StringVar(&spaceID, "space", "", "Space UUID (required only with multiple Spaces)")
	return command
}
