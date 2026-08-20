package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ApexReasoning/carry/internal/cli/userapi"
	"github.com/ApexReasoning/carry/internal/identity/memberfile"
	"github.com/ApexReasoning/carry/internal/machine/machinefile"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type enrollFlags struct {
	spaceID     string
	displayName string
}

func newEnrollCommand(configDirectory string, output io.Writer) *cobra.Command {
	var flags enrollFlags
	command := &cobra.Command{
		Use:   "enroll",
		Short: "Enroll this Machine using member authority",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runEnroll(command.Context(), configDirectory, output, flags)
		},
	}
	command.Flags().StringVar(&flags.spaceID, "space", "", "Space ID")
	command.Flags().StringVar(&flags.displayName, "name", "", "Machine display name")
	return command
}

func runEnroll(ctx context.Context, configDirectory string, output io.Writer, flags enrollFlags) error {
	if err := machinefile.RemoveRevoked(configDirectory); err != nil {
		return err
	}
	if _, err := machinefile.Load(configDirectory); err == nil {
		if err := machinefile.RemovePending(configDirectory); err != nil {
			return err
		}
		return machinefile.ErrAlreadyEnrolled
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	member, err := memberfile.Load(configDirectory)
	if err != nil {
		return err
	}
	connection, err := userapi.FromCredential(member)
	if err != nil {
		return err
	}
	info, err := connection.LoadMember(ctx)
	if err != nil {
		return fmt.Errorf("load current member: %w", err)
	}
	if info.UserID != member.UserID {
		return errors.New("current member identity does not match the saved login")
	}
	pending, err := loadOrCreatePendingEnrollment(configDirectory, member, info.Spaces, flags)
	if err != nil {
		return err
	}
	enrollment, err := connection.EnrollMachine(
		ctx,
		pending.SpaceID,
		pending.DisplayName,
		pending.IdempotencyKey,
		pending.PublicKeyDER,
	)
	if err != nil {
		return err
	}
	if err := machinefile.Save(configDirectory, machinefile.Credential{
		MachineID: enrollment.MachineID, SpaceID: enrollment.SpaceID, ServerURL: pending.ServerURL,
		CACertificatePEM: pending.CACertificatePEM, CertificatePEM: enrollment.CertificatePEM,
		PrivateKeyPEM: pending.PrivateKeyPEM,
	}); err != nil {
		return err
	}
	if err := machinefile.RemovePending(configDirectory); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(output, "Enrolled Machine %s\n", enrollment.MachineID)
	return nil
}

func loadOrCreatePendingEnrollment(
	configDirectory string,
	member memberfile.Credential,
	memberships []space.Membership,
	flags enrollFlags,
) (machinefile.PendingEnrollment, error) {
	pending, err := machinefile.LoadPending(configDirectory)
	if err == nil {
		if pending.ServerURL != member.ServerURL || pending.CACertificatePEM != member.CACertificatePEM {
			return machinefile.PendingEnrollment{}, errors.New("pending Machine enrollment belongs to a different Carry server")
		}
		if pending.EnrolledByUserID != member.UserID {
			return machinefile.PendingEnrollment{}, errors.New("pending Machine enrollment belongs to a different member")
		}
		if flags.spaceID != "" && flags.spaceID != pending.SpaceID {
			return machinefile.PendingEnrollment{}, errors.New("--space does not match the pending Machine enrollment")
		}
		if flags.displayName != "" && flags.displayName != pending.DisplayName {
			return machinefile.PendingEnrollment{}, errors.New("--name does not match the pending Machine enrollment")
		}
		if _, err := enrollmentSpace(memberships, pending.SpaceID); err != nil {
			return machinefile.PendingEnrollment{}, err
		}
		return pending, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return machinefile.PendingEnrollment{}, err
	}
	spaceID, err := enrollmentSpace(memberships, flags.spaceID)
	if err != nil {
		return machinefile.PendingEnrollment{}, err
	}
	displayName := flags.displayName
	if displayName == "" {
		displayName, err = os.Hostname()
		if err != nil {
			return machinefile.PendingEnrollment{}, fmt.Errorf("read hostname: %w", err)
		}
	}
	publicKeyDER, privateKeyPEM, err := machinefile.GenerateKey()
	if err != nil {
		return machinefile.PendingEnrollment{}, err
	}
	pending = machinefile.PendingEnrollment{
		ServerURL: member.ServerURL, CACertificatePEM: member.CACertificatePEM,
		EnrolledByUserID: member.UserID, SpaceID: spaceID,
		DisplayName: displayName, IdempotencyKey: uuid.NewString(),
		PublicKeyDER: publicKeyDER, PrivateKeyPEM: string(privateKeyPEM),
	}
	if err := machinefile.SavePending(configDirectory, pending); err != nil {
		return machinefile.PendingEnrollment{}, err
	}
	return pending, nil
}

func enrollmentSpace(memberships []space.Membership, requested string) (string, error) {
	if requested != "" {
		for _, membership := range memberships {
			if membership.SpaceID == requested && membership.CanEnrollMachines {
				return requested, nil
			}
		}
		return "", errors.New("current member cannot enroll a Machine in the selected Space")
	}
	var selected string
	for _, membership := range memberships {
		if !membership.CanEnrollMachines {
			continue
		}
		if selected != "" {
			return "", errors.New("--space is required when multiple Spaces allow Machine enrollment")
		}
		selected = membership.SpaceID
	}
	if selected == "" {
		return "", errors.New("no Space allows Machine enrollment")
	}
	return selected, nil
}
