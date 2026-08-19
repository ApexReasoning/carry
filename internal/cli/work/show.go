package work

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newShowCommand(configDirectory string, output io.Writer) *cobra.Command {
	var spaceID string
	command := &cobra.Command{
		Use:   "show <work-id>",
		Short: "Show Work and its messages",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			workID := arguments[0]
			if err := validateWorkID(workID); err != nil {
				return err
			}
			client, selectedSpaceID, err := connect(command.Context(), configDirectory, spaceID)
			if err != nil {
				return err
			}
			details, err := client.load(command.Context(), selectedSpaceID, workID)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(
				output, "Work %s\nGoal: %s\nStatus: %s\nOwner: %s\n",
				details.Work.WorkID, details.Work.Goal, details.Work.Lifecycle, details.Work.OwnerUserID,
			); err != nil {
				return err
			}
			if details.Work.Understanding == "" {
				if _, err := fmt.Fprintln(output, "Current understanding: not applied yet"); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintf(
				output,
				"Current understanding: %s\nNext step: %s\n",
				details.Work.Understanding,
				details.Work.NextStep,
			); err != nil {
				return err
			}
			if details.Work.HasUnappliedInput && details.Work.Understanding != "" {
				if _, err := fmt.Fprintln(output, "New information: not applied yet"); err != nil {
					return err
				}
			}
			if details.Work.NeedsRetry {
				if _, err := fmt.Fprintln(output, "Carry could not confirm an update. It will not try again until a member runs `carry work retry`."); err != nil {
					return err
				}
			}
			if len(details.Messages) == 0 {
				_, err = fmt.Fprintln(output, "Messages: none")
				return err
			}
			if _, err := fmt.Fprintln(output, "Messages:"); err != nil {
				return err
			}
			for _, message := range details.Messages {
				if _, err := fmt.Fprintf(output, "  %s: %s\n", message.AuthorUserID, message.Text); err != nil {
					return err
				}
			}
			return nil
		},
	}
	command.Flags().StringVar(&spaceID, "space", "", "Space UUID (required only with multiple Spaces)")
	return command
}
