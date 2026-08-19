package work

import (
	"fmt"
	"io"
	"strings"

	"github.com/ApexReasoning/carry/internal/identity/memberfile"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// NewCommand creates the member Work command group.
func NewCommand(configDirectory string, output io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "work",
		Short: "Create and follow durable Work",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(
		newCreateCommand(configDirectory, output),
		newListCommand(configDirectory, output),
		newShowCommand(configDirectory, output),
		newMessageCommand(configDirectory, output),
	)
	return command
}

func connect(configDirectory string, requestedSpaceID string) (*memberHTTP, string, error) {
	credential, err := memberfile.Load(configDirectory)
	if err != nil {
		return nil, "", fmt.Errorf("load member login: %w", err)
	}
	spaceID, err := selectSpace(credential, requestedSpaceID)
	if err != nil {
		return nil, "", err
	}
	client, err := connectMember(credential)
	if err != nil {
		return nil, "", fmt.Errorf("connect to Carry: %w", err)
	}
	return client, spaceID, nil
}

func selectSpace(credential memberfile.Credential, requestedSpaceID string) (string, error) {
	requestedSpaceID = strings.TrimSpace(requestedSpaceID)
	if requestedSpaceID != "" {
		if uuid.Validate(requestedSpaceID) != nil {
			return "", errorsInvalidSpaceID()
		}
		for _, membership := range credential.Spaces {
			if membership.SpaceID == requestedSpaceID {
				return requestedSpaceID, nil
			}
		}
		return "", fmt.Errorf("current member does not belong to Space %s", requestedSpaceID)
	}
	if len(credential.Spaces) == 1 {
		return credential.Spaces[0].SpaceID, nil
	}
	if len(credential.Spaces) == 0 {
		return "", fmt.Errorf("current member has no Space; run carry login again")
	}
	return "", fmt.Errorf("member belongs to multiple Spaces; select one with --space")
}

func validateWorkID(workID string) error {
	if uuid.Validate(workID) != nil {
		return fmt.Errorf("Work ID must be a UUID")
	}
	return nil
}

func errorsInvalidSpaceID() error {
	return fmt.Errorf("Space ID must be a UUID")
}
