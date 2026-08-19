package work

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

func newCreateCommand(configDirectory string, output io.Writer) *cobra.Command {
	var goal string
	var spaceID string
	command := &cobra.Command{
		Use:   "create",
		Short: "Create Work owned by the current member",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			goal = strings.TrimSpace(goal)
			if goal == "" {
				return fmt.Errorf("--goal is required")
			}
			client, selectedSpaceID, err := connect(command.Context(), configDirectory, spaceID)
			if err != nil {
				return err
			}
			pendingPath, idempotencyKey, err := pendingCreateIdentity(configDirectory, selectedSpaceID, goal)
			if err != nil {
				return err
			}
			created, err := client.create(command.Context(), selectedSpaceID, goal, idempotencyKey)
			if err != nil {
				return err
			}
			if err := clearPendingIdentity(pendingPath); err != nil {
				return fmt.Errorf("clear completed Work create identity: %w", err)
			}
			_, err = fmt.Fprintf(output, "Created Work %s\nGoal: %s\nOwner: %s\n", created.WorkID, created.Goal, created.OwnerUserID)
			return err
		},
	}
	command.Flags().StringVar(&goal, "goal", "", "one-sentence Work goal")
	command.Flags().StringVar(&spaceID, "space", "", "Space UUID (required only with multiple Spaces)")
	return command
}
