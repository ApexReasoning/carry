package work

import (
	"errors"
	"fmt"
	"io"

	"github.com/ApexReasoning/carry/internal/cli/userapi"
	"github.com/spf13/cobra"
)

func newRetryCommand(configDirectory string, output io.Writer) *cobra.Command {
	var spaceID string
	command := &cobra.Command{
		Use:   "retry <work-id>",
		Short: "Explicitly ask Carry to try this Work again",
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
			pendingPath, idempotencyKey, err := pendingRetryIdentity(configDirectory, selectedSpaceID, workID)
			if err != nil {
				return err
			}
			mutationErr := client.RetryWork(command.Context(), selectedSpaceID, workID, idempotencyKey)
			if mutationErr != nil {
				var unknown *userapi.OutcomeUnknownError
				if !errors.As(mutationErr, &unknown) {
					return mutationErr
				}
			}

			details, loadErr := client.LoadWork(command.Context(), selectedSpaceID, workID, "")
			if loadErr != nil {
				if mutationErr != nil {
					return fmt.Errorf("retry Work outcome is unknown; inspect the Work before trying again: %w", mutationErr)
				}
				return fmt.Errorf("load Work after retry: %w", loadErr)
			}
			if mutationErr != nil && details.Work.NeedsRetry {
				return fmt.Errorf("retry Work outcome is unknown; inspect the Work before trying again: %w", mutationErr)
			}
			if err := clearPendingIdentity(pendingPath); err != nil {
				return err
			}
			if details.Work.NeedsRetry {
				return errors.New("the previous retry was reconciled, but this Work needs a new choice; run the retry command again")
			}
			_, _ = fmt.Fprintf(output, "Carry will try Work %s again\n", workID)
			return nil
		},
	}
	command.Flags().StringVar(&spaceID, "space", "", "Space ID")
	return command
}
