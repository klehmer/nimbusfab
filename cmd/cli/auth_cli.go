package main

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/klehmer/nimbusfab/internal/inventory/sqlite"
	"github.com/klehmer/nimbusfab/internal/webapi/auth"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

func newUserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage local user accounts (Auth Phase 1 bootstrap)",
	}
	cmd.AddCommand(newUserCreateCommand())
	return cmd
}

func newUserCreateCommand() *cobra.Command {
	var email, password, displayName, orgID, dsn string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a local user account for the web UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			if email == "" || password == "" {
				return fmt.Errorf("--email and --password are required")
			}
			ctx := context.Background()
			repo, err := sqlite.Open(dsn)
			if err != nil {
				return fmt.Errorf("open repo: %w", err)
			}
			defer repo.Close()
			if err := repo.Migrate(ctx); err != nil {
				return fmt.Errorf("migrate: %w", err)
			}
			// Ensure org exists.
			if got, _ := repo.Orgs().Get(ctx, orgID); got == nil {
				if err := repo.Orgs().Create(ctx, inventory.Org{ID: orgID, Name: orgID}); err != nil {
					return fmt.Errorf("create org: %w", err)
				}
			}
			// Reject duplicate email.
			if got, _ := repo.Users().GetByEmail(ctx, orgID, email); got != nil {
				return fmt.Errorf("user with email %q already exists in org %q", email, orgID)
			}
			hash, err := auth.HashPassword(password)
			if err != nil {
				return fmt.Errorf("hash password: %w", err)
			}
			u := inventory.User{
				ID: "usr-" + uuid.NewString(), OrgID: orgID, Email: email,
				DisplayName: displayName, IsLocal: true, PasswordHash: hash,
			}
			if err := repo.Users().Create(ctx, u); err != nil {
				return fmt.Errorf("create user: %w", err)
			}
			fmt.Fprintf(os.Stdout, "Created user %s (id=%s) in org %s\n", email, u.ID, orgID)
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "user email (required)")
	cmd.Flags().StringVar(&password, "password", "", "user password (required)")
	cmd.Flags().StringVar(&displayName, "display-name", "", "human-readable name (optional)")
	cmd.Flags().StringVar(&orgID, "org", "default", "org ID")
	cmd.Flags().StringVar(&dsn, "inventory-dsn", "sqlite:./nimbusfab.db", "inventory DB DSN")
	return cmd
}

func newPATCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pat",
		Short: "Manage Personal Access Tokens (Auth Phase 1)",
	}
	cmd.AddCommand(newPATCreateCommand())
	return cmd
}

func newPATCreateCommand() *cobra.Command {
	var userID, name, orgID, dsn string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Mint a new PAT for a user. Prints the full token ONCE — copy immediately.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if userID == "" {
				return fmt.Errorf("--user-id is required")
			}
			if name == "" {
				name = "untitled-pat"
			}
			ctx := context.Background()
			repo, err := sqlite.Open(dsn)
			if err != nil {
				return fmt.Errorf("open repo: %w", err)
			}
			defer repo.Close()
			if err := repo.Migrate(ctx); err != nil {
				return fmt.Errorf("migrate: %w", err)
			}
			user, err := repo.Users().Get(ctx, orgID, userID)
			if err != nil {
				return fmt.Errorf("user lookup: %w", err)
			}
			if user == nil {
				return fmt.Errorf("user %q not found in org %q", userID, orgID)
			}
			token, prefix, hash, err := auth.GeneratePAT()
			if err != nil {
				return fmt.Errorf("generate PAT: %w", err)
			}
			row := inventory.ApiToken{
				ID: "pat-" + uuid.NewString(), OrgID: orgID, UserID: userID,
				Prefix: prefix, TokenHash: hash, Name: name,
			}
			if err := repo.ApiTokens().Create(ctx, row); err != nil {
				return fmt.Errorf("create PAT: %w", err)
			}
			fmt.Fprintln(os.Stdout, "PAT created. Copy this token now — it will NOT be shown again:")
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintf(os.Stdout, "    %s\n", token)
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintf(os.Stdout, "Use it as: Authorization: Bearer %s\n", token)
			fmt.Fprintf(os.Stdout, "Stored as id=%s prefix=%s\n", row.ID, prefix)
			return nil
		},
	}
	cmd.Flags().StringVar(&userID, "user-id", "", "user ID to attach the PAT to (required)")
	cmd.Flags().StringVar(&name, "name", "", "human label for the PAT (e.g. 'ci-script')")
	cmd.Flags().StringVar(&orgID, "org", "default", "org ID")
	cmd.Flags().StringVar(&dsn, "inventory-dsn", "sqlite:./nimbusfab.db", "inventory DB DSN")
	return cmd
}
