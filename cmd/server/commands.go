package main

import (
	"errors"
	"io"
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
	return true, errors.New("usage: server [maintenance|vault-rebuild|reembed]")
}
