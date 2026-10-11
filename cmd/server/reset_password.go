package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"reliquary/internal/application/service"
	"reliquary/internal/types"
)

// runServerCommand handles local maintenance before any server dependencies start.
func runServerCommand(args []string, input io.Reader, output io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if args[0] == "maintenance" {
		return true, runMaintenance(args[1:], input, output)
	}
	if len(args) == 1 && args[0] == "vault-rebuild" {
		return true, runVaultRebuild(output)
	}
	if args[0] == "reembed" {
		return true, runReembed(args[1:], output)
	}
	if len(args) != 1 || args[0] != "reset-password" {
		return true, errors.New("usage: server [reset-password|vault-rebuild|reembed]")
	}
	fmt.Fprintln(output, "Enter the new password:")
	reader := bufio.NewReader(input)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return true, fmt.Errorf("read password: %w", err)
	}
	password := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if err := service.ValidatePassword(password); err != nil {
		return true, err
	}
	driver := strings.TrimSpace(os.Getenv("DB_DRIVER"))
	if driver != "" && driver != "postgres" {
		return true, fmt.Errorf("unsupported database driver: %s", driver)
	}
	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "5432"
	}
	dsn := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port),
		User: url.UserPassword(os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD")), Path: "/" + os.Getenv("DB_NAME")}
	query := dsn.Query()
	query.Set("sslmode", "disable")
	query.Set("connect_timeout", "10")
	dsn.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(dsn.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return true, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	pool, err := db.DB()
	if err != nil {
		return true, err
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := resetUserPassword(ctx, db, password); err != nil {
		return true, err
	}
	fmt.Fprintln(output, "Password reset. Existing login sessions have been revoked.")
	return true, nil
}

func resetUserPassword(ctx context.Context, db *gorm.DB, password string) error {
	if err := service.ValidatePassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var users []types.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Limit(2).Find(&users).Error; err != nil {
			return err
		}
		if len(users) != 1 || users[0].TenantID != types.SingleTenantID {
			return errors.New("password reset requires exactly one registered user")
		}
		if err := tx.Model(&users[0]).Update("password_hash", string(hash)).Error; err != nil {
			return err
		}
		return tx.Model(&types.AuthToken{}).Where("user_id = ?", users[0].ID).Update("is_revoked", true).Error
	})
}
