package main

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

func TestResetPasswordCommandChangesLoginAndRevokesSessions(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	if err := service.EnsureSingleTeamSpace(ctx, db); err != nil {
		t.Fatal(err)
	}
	tenantService := service.NewTenantService(repository.NewTenantRepository(db))
	userService := service.NewUserService(&config.Config{}, repository.NewUserRepository(db), repository.NewAuthTokenRepository(db), tenantService)
	user, err := userService.Register(ctx, &types.RegisterRequest{Username: "personal", Email: "personal@example.com", Password: "old-password"})
	if err != nil {
		t.Fatal(err)
	}
	oldSession, err := userService.Login(ctx, &types.LoginRequest{Email: user.Email, Password: "old-password"})
	if err != nil || !oldSession.Success {
		t.Fatalf("initial login: response=%+v err=%v", oldSession, err)
	}
	var databaseName string
	if err := db.Raw("SELECT current_database()").Scan(&databaseName).Error; err != nil {
		t.Fatal(err)
	}
	dsn, err := url.Parse(os.Getenv(pgtest.DSNEnv))
	if err != nil {
		t.Fatal(err)
	}
	password, _ := dsn.User.Password()
	t.Setenv("DB_DRIVER", "postgres")
	t.Setenv("DB_HOST", dsn.Hostname())
	t.Setenv("DB_PORT", dsn.Port())
	t.Setenv("DB_USER", dsn.User.Username())
	t.Setenv("DB_PASSWORD", password)
	t.Setenv("DB_NAME", databaseName)
	var output bytes.Buffer
	handled, err := runServerCommand([]string{"reset-password"}, strings.NewReader("new-password\n"), &output)
	if !handled || err != nil {
		t.Fatalf("command handled=%v err=%v output=%s", handled, err, output.String())
	}
	if strings.Contains(output.String(), "new-password") {
		t.Fatal("command disclosed password")
	}
	newLogin, err := userService.Login(ctx, &types.LoginRequest{Email: user.Email, Password: "new-password"})
	if err != nil || !newLogin.Success {
		t.Fatalf("new password login: response=%+v err=%v", newLogin, err)
	}
	if _, _, err := userService.ValidateToken(ctx, newLogin.Token); err != nil {
		t.Fatalf("new login token is invalid: %v", err)
	}
	oldLogin, err := userService.Login(ctx, &types.LoginRequest{Email: user.Email, Password: "old-password"})
	if err != nil || oldLogin.Success {
		t.Fatalf("old password login: response=%+v err=%v", oldLogin, err)
	}
	if _, _, err := userService.ValidateToken(ctx, oldSession.Token); err == nil {
		t.Fatal("old access token remained valid")
	}
	if _, _, err := userService.RefreshToken(ctx, oldSession.RefreshToken); err == nil {
		t.Fatal("old refresh token remained valid")
	}
}

func TestResetPasswordRejectsInvalidInputBeforeConnecting(t *testing.T) {
	for _, args := range [][]string{{"reset-password", "secret-in-argument"}, {"unknown"}} {
		if handled, err := runServerCommand(args, strings.NewReader("valid-password\n"), &bytes.Buffer{}); !handled || err == nil {
			t.Fatalf("args %v: handled=%v err=%v", args, handled, err)
		}
	}
	if handled, err := runServerCommand([]string{"reset-password"}, strings.NewReader("short\n"), &bytes.Buffer{}); !handled || err == nil {
		t.Fatalf("short input: handled=%v err=%v", handled, err)
	}
}
