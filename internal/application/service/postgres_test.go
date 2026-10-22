package service

import (
	"fmt"
	"os"
	"testing"

	"reliquary/internal/config"
	"reliquary/internal/testutil/pgtest"
)

func TestMain(m *testing.M) {
	if err := config.LoadPromptTemplates("../../../config"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(pgtest.Run(m))
}
