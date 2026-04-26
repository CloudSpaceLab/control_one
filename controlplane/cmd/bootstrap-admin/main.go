// Bootstrap-admin seeds the four built-in operator accounts so a brand-
// new deploy is immediately usable without poking the API. Passwords are
// generated, printed once to stdout, and persisted to the local users
// table via storage.CreateLocalUser.
//
// Run AFTER `migrate.Apply` so the roles + permissions catalog from
// migration 0059 already exists. The storage layer will idempotently
// upsert each account so re-running is safe — passwords reset on each
// run unless --skip-existing is set.
//
// Default accounts seeded:
//   admin@local     → role=admin    (full access)
//   ciso@local      → role=ciso     (RBAC + policies + forensics)
//   operator@local  → role=operator (alerts + remediation + rules)
//   viewer@local    → role=viewer   (read-only)
//
// Output (stdout): a JSON array of {email, password, role} so the
// deploy script can capture + display them to the operator.

package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/migrate"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type seedAccount struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Password    string `json:"password"`
}

func defaultSeedAccounts() []seedAccount {
	return []seedAccount{
		{Email: "admin@local", DisplayName: "Default Admin", Role: "admin"},
		{Email: "ciso@local", DisplayName: "Default CISO Admin", Role: "ciso"},
		{Email: "operator@local", DisplayName: "Default Operator", Role: "operator"},
		{Email: "viewer@local", DisplayName: "Default Viewer", Role: "viewer"},
	}
}

func main() {
	configPath := flag.String("config", "config/controlplane.yaml", "path to control plane config file")
	subject := flag.String("subject", "", "(legacy) external subject for a single bootstrap admin")
	name := flag.String("name", "", "(legacy) display name for the bootstrap admin user")
	email := flag.String("email", "", "(legacy) email for the bootstrap admin user")
	applyMigrations := flag.Bool("apply-migrations", false, "run database migrations first")
	seedDefaults := flag.Bool("seed-defaults", false, "seed the four built-in operator accounts (admin/ciso/operator/viewer)")
	skipExisting := flag.Bool("skip-existing", false, "skip accounts that already have a password hash on file")
	jsonOut := flag.Bool("json", false, "emit the seeded credentials as a single JSON array on stdout")
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("load config", zap.Error(err))
	}

	store, err := storage.New(logger, cfg.Database, storage.Options{})
	if err != nil {
		logger.Fatal("connect database", zap.Error(err))
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Warn("close database", zap.Error(err))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if *applyMigrations {
		logger.Info("applying database migrations")
		if err := migrate.Apply(ctx, store.DB()); err != nil {
			logger.Fatal("apply migrations", zap.Error(err))
		}
	}

	// Legacy single-admin path — kept for backwards compat with the
	// existing deploy.py invocation.
	if strings.TrimSpace(*subject) != "" && !*seedDefaults {
		user, err := store.EnsureUser(ctx, strings.TrimSpace(*subject), strings.TrimSpace(*email), strings.TrimSpace(*name))
		if err != nil {
			logger.Fatal("ensure user", zap.Error(err))
		}
		if err := store.AssignRolesToUser(ctx, user.ID, []string{"admin"}); err != nil {
			logger.Fatal("assign admin role", zap.Error(err))
		}
		fmt.Printf("Bootstrap admin ensured (subject=%s, id=%s)\n", user.ExternalID, user.ID)
		return
	}

	if !*seedDefaults {
		fmt.Fprintln(os.Stderr, "either --seed-defaults or --subject is required")
		os.Exit(1)
	}

	out := make([]seedAccount, 0, 4)
	for _, acc := range defaultSeedAccounts() {
		acc := acc
		// Skip if --skip-existing AND the account already has a hash.
		if *skipExisting {
			existing, _ := store.GetLocalUserByEmail(ctx, acc.Email)
			if existing != nil && existing.PasswordChangedAt.Valid {
				continue
			}
		}
		acc.Password = generatePassword(20)
		_, err := store.CreateLocalUser(ctx, storage.CreateLocalUserParams{
			Email:       acc.Email,
			DisplayName: acc.DisplayName,
			Password:    acc.Password,
			Provider:    storage.AuthProviderLocal,
			Roles:       []string{acc.Role},
		})
		if err != nil {
			logger.Error("seed account", zap.String("email", acc.Email), zap.Error(err))
			continue
		}
		out = append(out, acc)
	}

	if *jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(out)
		return
	}
	fmt.Println()
	fmt.Println("=== Seeded operator accounts ===")
	for _, a := range out {
		fmt.Printf("  %-18s  password: %s   (role: %s)\n", a.Email, a.Password, a.Role)
	}
	fmt.Println()
}

// generatePassword returns a base64-url string of n bytes of entropy.
// 20 bytes → ~27 chars, ~160 bits — enough that brute-forcing is hopeless
// even if bcrypt cost dropped to default.
func generatePassword(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// fail safe rather than fail open
		panic("rand.Read: " + err.Error())
	}
	return strings.TrimRight(base64.URLEncoding.EncodeToString(buf), "=")
}
