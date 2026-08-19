package work

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

func newMessageCommand(configDirectory string, output io.Writer) *cobra.Command {
	var text string
	var spaceID string
	command := &cobra.Command{
		Use:   "message <work-id>",
		Short: "Add a message to Work",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			workID := arguments[0]
			if err := validateWorkID(workID); err != nil {
				return err
			}
			if strings.TrimSpace(text) == "" {
				return fmt.Errorf("--text is required")
			}
			client, selectedSpaceID, err := connect(configDirectory, spaceID)
			if err != nil {
				return err
			}
			pendingPath, idempotencyKey, err := pendingMessageIdentity(
				configDirectory, selectedSpaceID, workID, text,
			)
			if err != nil {
				return err
			}
			message, err := client.appendMessage(
				command.Context(), selectedSpaceID, workID, text, idempotencyKey,
			)
			if err != nil {
				return err
			}
			if err := clearPendingIdentity(pendingPath); err != nil {
				return fmt.Errorf("clear completed Work Message identity: %w", err)
			}
			_, err = fmt.Fprintf(output, "Added input %d to Work %s\n", message.InputSeq, workID)
			return err
		},
	}
	command.Flags().StringVar(&text, "text", "", "message text")
	command.Flags().StringVar(&spaceID, "space", "", "Space UUID (required only with multiple Spaces)")
	return command
}
