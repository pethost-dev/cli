package kernel

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	pethost "github.com/pethost-dev/sdk/go"
	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/cmd/account"
	"github.com/pethost-dev/cli/internal/fail"
	"github.com/pethost-dev/cli/internal/operation"
	"github.com/pethost-dev/cli/internal/target"
)

// deployCommand is `pethost deploy [dir]`: it puts a folder online, and again is a new version.
// Signing in, the plan, packing, the upload, the build and the address happen inside it, in
// order, each shown as a line; the URL alone goes to stdout.
func deployCommand(a *app.App) *cobra.Command {
	d := &deploy{a: a}
	cmd := &cobra.Command{
		Use:   "deploy [dir]",
		Short: "Put a folder online; again, a new version",
		Long: `Put a folder online: this one, or the one named. The first time it asks for the
project's name, which becomes its address; again, it deploys a new version.

The folder needs a Dockerfile or a compose file. Everything in it is sent but what a
build makes again, and the machine builds and runs it.

Where the folder has neither, pethost writes the Dockerfile itself for a project of a
kind it knows: the file's first comment says what it read that from. A project it does
not know it offers to a coding agent on this computer (Claude Code, Codex, pi), once
you chose one; PETHOST_AGENT names one for a run, and PETHOST_AGENT=none turns that
off. A Dockerfile it wrote is yours once its first deploy has built and started: it is
never written again. One that fails there is taken back, and you are told what failed.`,
		Example: `  pethost deploy
  pethost deploy ./api
  pethost deploy -p recipes        in a script: nothing is asked
  pethost deploy --private         no public address`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.run(cmd.Context(), args)
		},
	}
	cmd.Flags().BoolVar(&d.private, "private", false, "give a new project no public address")
	d.noQueue = operation.NoQueueFlag(cmd)
	return cmd
}

// A deploy is one run of the command.
type deploy struct {
	a       *app.App
	private bool
	noQueue *bool

	client *pethost.Client
	// The machine and its projects, as GetMachine answered.
	machine *pethost.GetMachineResponse
	// The folder that is deployed, and its .pethost if it has one.
	dir  string
	file *target.File
	// The folder is not the one the person stands in: a .pethost above it made its folder the
	// project's.
	fromAbove bool
	// What the folder holds, read before anything is asked, and the transfer its names to leave
	// out came with: the upload's, while its request can still start.
	packed   *contents
	transfer *pethost.CreateTransferResponse
	// The project, and whether this deploy creates it.
	project target.Project
	create  bool
	// The folder gets a .pethost: it had none, or one that named another machine's project.
	writeFile bool
	// The person signed in during this run.
	signedInNow bool
	// What this run wrote from its own reading of the folder, which its first deploy proves
	// (deploy_proof.go); nil = nothing, or what runs the folder is the person's or their agent's.
	own *attempt
	// The answer of the command's last call, which --json prints.
	answer any
}

// run is the deploy in its steps.
func (d *deploy) run(ctx context.Context, args []string) error {
	if err := d.signIn(ctx); err != nil {
		return err
	}
	if err := d.readMachine(ctx); err != nil {
		return err
	}
	if err := d.chooseFolder(args); err != nil {
		return err
	}
	if err := d.lookAtFolder(ctx); err != nil {
		return err
	}
	if err := d.chooseProject(); err != nil {
		return err
	}

	started, err := d.sendAndStart(ctx)
	if err != nil {
		return err
	}
	if err := d.rememberFolder(); err != nil {
		return err
	}

	return d.watch(ctx, started)
}

// watch shows a started deploy to its end and says how it ended. Whether it builds an image
// the API says only once it ended: the title claims no more than is known.
func (d *deploy) watch(ctx context.Context, started *pethost.Operation) error {
	result, err := operation.Watch(ctx, d.a, d.project, started, "Deploying…")
	if err != nil {
		return err
	}
	d.answer = result.Answer
	return d.finish(ctx, result)
}

// sendAndStart uploads the folder and starts its deploy. A folder the machine finds nothing to
// run in is first made runnable where that can be done (deploy_runnable.go), and sent again.
func (d *deploy) sendAndStart(ctx context.Context) (*pethost.Operation, error) {
	started, err := d.sendAsItIs(ctx)
	if nothing, _ := errors.AsType[*nothingToRun](err); nothing != nil {
		return d.sendMadeRunnable(ctx, nothing)
	}
	return started, err
}

// sendAsItIs uploads the folder as it is and starts its deploy. A new project's name that the
// machine cannot take is asked for again: the rule for names is the machine's alone, and it
// applies it when the project is created. The folder is not sent again: a request the machine
// refused for its values left the upload where it was.
func (d *deploy) sendAsItIs(ctx context.Context) (*pethost.Operation, error) {
	uploadID, err := d.send(ctx)
	if err != nil {
		return nil, err
	}
	for {
		started, err := d.start(ctx, uploadID)
		refusal := refusedName(err)
		if refusal == "" || !d.create || d.project.Source != target.Picked || !d.a.UI.CanAsk() {
			return started, err
		}
		d.a.UI.Problem("%s", refusal)
		if err := d.nameNew(""); err != nil {
			return nil, err
		}
	}
}

// signIn makes sure there is a sign-in: on a terminal deploy signs the person in itself.
func (d *deploy) signIn(ctx context.Context) error {
	current, err := d.a.SignIn()
	if err != nil {
		return err
	}
	if current.Token == "" {
		if !d.a.UI.CanAsk() {
			return fail.NotSignedIn()
		}
		if err := account.SignIn(ctx, d.a); err != nil {
			return err
		}
		d.signedInNow = true
	}

	d.a.SayToken()
	d.client, err = d.a.Client()
	return err
}

// chooseFolder settles which folder is deployed: the one named; else, when nothing names the
// project, the folder whose .pethost does, which may be above this one; else this one.
func (d *deploy) chooseFolder(args []string) error {
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fail.Failed("%s is not a folder.", dir)
	}
	// A link to a folder is deployed as the folder it leads to.
	if dir, err = filepath.EvalSymlinks(dir); err != nil {
		return err
	}

	d.dir = dir
	if len(args) == 1 || d.namedOutside() {
		d.file, err = target.ReadFile(dir)
		return err
	}

	d.file, err = target.FindFile(dir)
	if d.file != nil && d.file.Dir != dir {
		d.dir, d.fromAbove = d.file.Dir, true
	}
	return err
}

// namedOutside says whether -p or PETHOST_PROJECT names the project, whatever the folder says.
func (d *deploy) namedOutside() bool {
	return d.a.Project != "" || os.Getenv("PETHOST_PROJECT") != ""
}

// rememberFolder writes the folder's .pethost once the machine has taken the deploy: from then
// on the project exists, however its first deploy ends. The commands the deploy names from
// here on then need no -p where the person stands in the folder.
func (d *deploy) rememberFolder() error {
	if d.writeFile {
		domain := ""
		if d.machine.Machine != nil {
			domain = d.machine.Machine.AppsDomain
		}
		if err := target.WriteFile(d.dir, d.project.ID, domain); err != nil {
			return fail.Failed("The deploy runs, but %s could not be written here: %s", target.FileName, err).
				WithLead("Follow it:", "pethost logs --deploy -f"+d.project.Flag())
		}
	}

	switch here, err := target.FindFile("."); {
	case d.project.Source == target.FromEnvironment:
	case err == nil && here != nil && here.ProjectID == d.project.ID:
		d.project.Source = target.FromFile
	default:
		d.project.Source = target.Picked
	}
	return nil
}
