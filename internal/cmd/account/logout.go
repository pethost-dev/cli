package account

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/pethost-dev/cli/internal/app"
	"github.com/pethost-dev/cli/internal/oauth"
	"github.com/pethost-dev/cli/internal/signin"
)

// logoutCommand is `pethost logout`: this computer forgets its sign-in. A sign-in made in the
// browser is revoked at the panel as well, where the panel offers that; otherwise the command
// says where the person revokes it.
func logoutCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget this computer's sign-in",
		Long: `Forget this computer's sign-in. Nothing on your machine changes: your projects go
on running.`,
		Example: `  pethost logout`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return logout(cmd.Context(), a)
		},
	}
}

func logout(ctx context.Context, a *app.App) error {
	stored, err := signin.Load()
	if err != nil {
		return err
	}
	if stored == nil || stored.Token == "" {
		a.UI.Info("This computer has no sign-in to forget.")
		sayTokenStays(a)
		return nil
	}

	revoked := revoke(ctx, stored)
	if err := signin.Remove(); err != nil {
		return err
	}

	switch {
	case revoked:
		a.UI.Done("Signed out: the panel no longer takes this computer's sign-in.")
	case stored.ClientID != "":
		a.UI.Done("Signed out on this computer.")
		a.UI.Info("  The sign-in itself works until you revoke it: %s/settings, under Agents.", stored.URL)
	default:
		a.UI.Done("Signed out on this computer.")
		a.UI.Info("  The API token itself works until you revoke it: %s/settings, under API tokens.", stored.URL)
	}
	sayTokenStays(a)
	return nil
}

// revokeWait bounds the two requests of a revocation: signing out must not hang on a panel
// that does not answer.
const revokeWait = 10 * time.Second

// revoke ends the sign-in at the panel, and says whether it did. Only a sign-in made in the
// browser is this program's own to end: an API token is the person's, and may be in use
// elsewhere.
func revoke(ctx context.Context, stored *signin.Stored) bool {
	if stored.ClientID == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, revokeWait)
	defer cancel()

	server, err := oauth.Discover(ctx, stored.URL)
	if err != nil || server.RevocationEndpoint == "" {
		return false
	}
	return server.Revoke(ctx, stored.ClientID, stored.Token) == nil
}

// sayTokenStays says that PETHOST_TOKEN is not this command's to forget.
func sayTokenStays(a *app.App) {
	if os.Getenv("PETHOST_TOKEN") != "" {
		a.UI.Info("PETHOST_TOKEN is still set: commands go on using it.")
	}
}
