package sandbox

import (
	"fmt"
	"strings"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/ui"
)

// volumeCommand is `pethost sandbox volume`, which lists, and the commands under it.
func volumeCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "volume",
		Aliases: []string{"volumes"},
		Short:   "Volumes: disks that outlive sandboxes, mounted into them",
		Long: `Lists the workspace's volumes: disks outside every sandbox, which a sandbox mounts at a
path when it is made (pethost sandbox new --mount <id>:/data). One sandbox at a time mounts
a volume read-write, any number read-only. A volume costs its whole size, used or not.`,
		Example: `  pethost sandbox volume
  pethost sandbox volume new data --size 10
  pethost sandbox volume rm vol_8f2k3j`,
		Args: cobra.NoArgs,
	}
	list := volumeListCommand(a)
	cmd.RunE = list.RunE
	cmd.AddCommand(list, volumeNewCommand(a), volumeRmCommand(a))
	return cmd
}

func volumeListCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Short:   "The workspace's volumes",
		Example: `  pethost sandbox volume ls`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client(a)
			if err != nil {
				return err
			}
			all := &sandboxes.ListVolumesResponse{}
			request := &sandboxes.ListVolumesRequest{PageSize: 500}
			for {
				page, err := c.ListVolumes(cmd.Context(), request)
				if err != nil {
					return err
				}
				all.Volumes = append(all.Volumes, page.Volumes...)
				if page.NextPageToken == "" || len(page.Volumes) == 0 {
					break
				}
				request.PageToken = page.NextPageToken
			}
			if a.JSON {
				return a.PrintJSON(all)
			}
			printVolumes(a, all.Volumes)
			return nil
		},
	}
}

func printVolumes(a *app.App, volumes []sandboxes.Volume) {
	if len(volumes) == 0 {
		a.UI.Info("No volume here yet. Make one:  %s", a.UI.Err.Bold("pethost sandbox volume new data --size 10"))
		return
	}
	out := a.UI.Out
	table := out.Table()
	for _, volume := range volumes {
		mounted := out.Dim("mounted by no sandbox")
		if len(volume.MountedBy) > 0 {
			mounted = "mounted by " + strings.Join(cleaned(volume.MountedBy), ", ")
		}
		table.Row(out.Bold(ui.Clean(volume.VolumeID)), ui.Clean(volume.Name),
			fmt.Sprintf("%s of %d GiB", ui.Size(uint64(volume.UsedBytes)), volume.SizeGib), mounted)
	}
	table.Flush()
}

func volumeNewCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Make a volume of a size in GiB",
		Long: `Makes a volume of --size GiB, on one server: a sandbox that mounts it runs there. Its id
is the answer.`,
		Example: `  pethost sandbox volume new data --size 10`,
		Args:    cobra.ExactArgs(1),
	}
	var size int32
	cmd.Flags().Int32Var(&size, "size", 0, "its size in GiB")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := clientForChange(a)
		if err != nil {
			return err
		}
		volume, err := c.CreateVolume(cmd.Context(), &sandboxes.CreateVolumeRequest{Name: args[0], SizeGib: size})
		if err != nil {
			return err
		}
		if a.JSON {
			return a.PrintJSON(volume)
		}
		a.UI.Done("Made the volume %s, %d GiB. Mount it:  %s", ui.Clean(volume.Name), volume.SizeGib,
			a.UI.Err.Bold("pethost sandbox new --mount "+ui.Clean(volume.VolumeID)+":/data"))
		_, err = fmt.Fprintln(a.UI.Out, ui.Clean(volume.VolumeID))
		return err
	}
	return cmd
}

func volumeRmCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Delete a volume and its data",
		Long: `Deletes a volume and its data for good; one a sandbox mounts is refused. It asks first,
and needs --yes where nobody can be asked.`,
		Example: `  pethost sandbox volume rm vol_8f2k3j --yes`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := clientForChange(a)
			if err != nil {
				return err
			}
			question := fmt.Sprintf("Delete the volume %s and its data for good?", ui.Clean(args[0]))
			if yes, err := a.UI.Confirm(question, a.CommandLine("--yes")); err != nil || !yes {
				return declined(err, "Nothing was deleted.")
			}
			volume, err := c.DeleteVolume(cmd.Context(), &sandboxes.DeleteVolumeRequest{VolumeID: args[0]})
			if err != nil {
				return err
			}
			if a.JSON {
				return a.PrintJSON(volume)
			}
			_, err = fmt.Fprintln(a.UI.Out, ui.Clean(volume.VolumeID))
			return err
		},
	}
}
