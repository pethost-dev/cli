package sandbox

import (
	"context"
	"fmt"

	"github.com/pethost-dev/sdk/go/sandboxes"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/ui"
)

// templateCommand is `pethost sandbox template`, which lists, and the commands under it.
func templateCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "template",
		Aliases: []string{"templates"},
		Short:   "Templates: what a sandbox starts from; save one from a sandbox",
		Long: `Lists the templates a sandbox can start from: ours, as base, and the workspace's, each
one's newest version. A template saved from a sandbox holds its files and settings, never
its memory or processes: pethost sandbox new --template <name> starts from it.`,
		Example: `  pethost sandbox template
  pethost sandbox template save k7f3q2xy0a1b2c3d my-app
  pethost sandbox template ls my-app`,
		Args: cobra.NoArgs,
	}
	list := templateListCommand(a)
	cmd.RunE = list.RunE
	cmd.AddCommand(list, templateSaveCommand(a), templateShowCommand(a), templateRmCommand(a))
	return cmd
}

func templateListCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "ls [name]",
		Short: "The templates, or one template's versions",
		Example: `  pethost sandbox template ls
  pethost sandbox template ls my-app`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client(a)
			if err != nil {
				return err
			}
			request := &sandboxes.ListTemplatesRequest{PageSize: 500}
			if len(args) == 1 {
				request.Name = args[0]
			}
			all, err := allTemplates(cmd.Context(), c, request)
			if err != nil {
				return err
			}
			if a.JSON {
				return a.PrintJSON(all)
			}
			printTemplates(a, all.Templates)
			return nil
		},
	}
}

// allTemplates is every page of ListTemplates as one answer.
func allTemplates(ctx context.Context, c *sandboxes.Client, request *sandboxes.ListTemplatesRequest) (*sandboxes.ListTemplatesResponse, error) {
	all := &sandboxes.ListTemplatesResponse{}
	for {
		page, err := c.ListTemplates(ctx, request)
		if err != nil {
			return nil, err
		}
		all.Templates = append(all.Templates, page.Templates...)
		if page.NextPageToken == "" || len(page.Templates) == 0 {
			return all, nil
		}
		request.PageToken = page.NextPageToken
	}
}

func printTemplates(a *app.App, templates []sandboxes.Template) {
	out := a.UI.Out
	table := out.Table()
	for i := range templates {
		template := &templates[i]
		size := ""
		if template.SizeBytes > 0 {
			size = ui.Size(uint64(template.SizeBytes))
		}
		owner := ""
		if template.BuiltIn {
			owner = out.Dim("ours")
		}
		table.Row(out.Bold(versionOf(template)), word(template.State), size, owner, out.Ago(template.CreateTime), oneLine(template.Description))
	}
	table.Flush()
}

// versionOf is a template's version as CreateSandbox takes it: "my-app@3".
func versionOf(template *sandboxes.Template) string {
	return fmt.Sprintf("%s@%d", ui.Clean(template.Name), template.Version)
}

func templateSaveCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "save <id> <name>",
		Short: "Save a sandbox's files and settings as a template's next version",
		Long: `Saves the sandbox's disk and settings as the next version of the template of that name,
a new template if the workspace has none of it. A running sandbox goes on running. The
version is the answer, "name@version"; it is ready once uploaded, some seconds a GiB.`,
		Example: `  pethost sandbox template save k7f3q2xy0a1b2c3d my-app
  pethost sandbox template save k7f3q2xy0a1b2c3d my-app --description 'Node 24 and our deps'`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeSandboxes(a),
	}
	var description string
	cmd.Flags().StringVar(&description, "description", "", "what it holds, for people")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := clientForChange(a)
		if err != nil {
			return err
		}
		id := idOf(cmd.Context(), c, args[0])
		template, err := c.CreateTemplate(cmd.Context(), &sandboxes.CreateTemplateRequest{SandboxID: id, Name: args[1], Description: description})
		if err != nil {
			return about(id, err)
		}
		if a.JSON {
			return a.PrintJSON(template)
		}
		version := versionOf(template)
		a.UI.Done("Saving %s: it is ready once uploaded. See:  %s", version, a.UI.Err.Bold("pethost sandbox template show "+version))
		_, err = fmt.Fprintln(a.UI.Out, version)
		return err
	}
	return cmd
}

func templateShowCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show <name[@version]>",
		Short: "One template version: its state, size and settings",
		Example: `  pethost sandbox template show my-app
  pethost sandbox template show my-app@3 --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client(a)
			if err != nil {
				return err
			}
			template, err := c.GetTemplate(cmd.Context(), &sandboxes.GetTemplateRequest{Template: args[0]})
			if err != nil {
				return err
			}
			if a.JSON {
				return a.PrintJSON(template)
			}
			printTemplate(a, template)
			return nil
		},
	}
}

func printTemplate(a *app.App, template *sandboxes.Template) {
	out := a.UI.Out
	table := out.Table()
	table.Row("version", out.Bold(versionOf(template)))
	table.Row("state", word(template.State))
	if template.FailureMessage != "" {
		table.Row("", ui.Clean(template.FailureMessage))
	}
	if template.SizeBytes > 0 {
		table.Row("size", ui.Size(uint64(template.SizeBytes)))
	}
	if template.MinSize != sandboxes.SizeUnspecified {
		table.Row("smallest size", word(template.MinSize))
	}
	if template.Description != "" {
		table.Row("description", ui.Clean(template.Description))
	}
	if settings := template.Settings; settings != nil && settings.Size != sandboxes.SizeUnspecified {
		table.Row("starts at", word(settings.Size))
	}
	table.Row("created", out.When(template.CreateTime))
	table.Flush()
}

func templateRmCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a template, every version of it",
		Long: `Deletes every version of the template: no sandbox starts from it any more, and what it
takes stops counting at once. Sandboxes made from it go on. It asks first, and needs --yes
where nobody can be asked.`,
		Example: `  pethost sandbox template rm my-app
  pethost sandbox template rm my-app --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := clientForChange(a)
			if err != nil {
				return err
			}
			question := fmt.Sprintf("Delete the template %s, every version of it?", ui.Clean(args[0]))
			if yes, err := a.UI.Confirm(question, a.CommandLine("--yes")); err != nil || !yes {
				return declined(err, "Nothing was deleted.")
			}
			template, err := c.DeleteTemplate(cmd.Context(), &sandboxes.DeleteTemplateRequest{Name: args[0]})
			if err != nil {
				return err
			}
			if a.JSON {
				return a.PrintJSON(template)
			}
			_, err = fmt.Fprintln(a.UI.Out, ui.Clean(template.Name))
			return err
		},
	}
}
