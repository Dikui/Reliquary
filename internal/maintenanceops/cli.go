package maintenanceops

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"gorm.io/gorm"
)

// RunCLI handles the state subcommands beneath server maintenance. File export,
// verification and rebuild remain in the maintenance package.
func RunCLI(ctx context.Context, db *gorm.DB, args []string, output io.Writer, flush func(context.Context) error) error {
	if len(args) == 0 {
		return errors.New("maintenance requires pause, resume, status, record, configure or due")
	}
	flags := flag.NewFlagSet("maintenance "+args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	emit := func(value any) error { return json.NewEncoder(output).Encode(value) }
	switch args[0] {
	case "pause":
		mode := flags.String("mode", "snapshot", "drain or snapshot")
		timeout := flags.Duration("timeout", 300*time.Second, "maximum wait")
		reason := flags.String("reason", "maintenance", "operation name")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *timeout <= 0 {
			return errors.New("pause timeout must be positive")
		}
		wait, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()
		if err := Pause(wait, db, *mode, *reason, flush); err != nil {
			return err
		}
		return emit(map[string]any{"paused": true, "mode": *mode, "next": "stop the application, flush again, then export"})
	case "resume":
		if err := Resume(ctx, db); err != nil {
			return err
		}
		return emit(map[string]bool{"paused": false})
	case "status":
		state, err := Status(ctx, db)
		if err != nil {
			return err
		}
		return emit(state)
	case "record":
		kind := flags.String("kind", "backup", "backup or drill")
		file := flags.String("result", "", "JSON result file")
		status := flags.String("status", "success", "result status")
		path := flags.String("path", "", "backup path")
		size := flags.Int64("size", 0, "snapshot bytes")
		message := flags.String("error", "", "failure explanation")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		result := Result{Status: *status, Path: *path, SizeBytes: *size, Error: *message}
		if *file != "" {
			data, err := os.ReadFile(*file)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, &result); err != nil {
				return fmt.Errorf("read maintenance result: %w", err)
			}
		}
		if err := Record(ctx, db, *kind, result); err != nil {
			return err
		}
		return emit(result)
	case "configure":
		state, err := Status(ctx, db)
		if err != nil {
			return err
		}
		backup := flags.String("backup-schedule", state.BackupSchedule, "five-field cron")
		drill := flags.String("drill-schedule", state.DrillSchedule, "five-field cron")
		zone := flags.String("timezone", state.ScheduleTimezone, "IANA timezone")
		dir := flags.String("backup-directory", state.BackupDirectory, "snapshot directory")
		enabled := flags.Bool("enabled", state.AutomaticEnabled, "enable automatic jobs")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := Configure(ctx, db, ScheduleConfig{*backup, *drill, *zone, *dir, *enabled}); err != nil {
			return err
		}
		state, err = Status(ctx, db)
		if err != nil {
			return err
		}
		return emit(state)
	case "due":
		force := flags.Bool("force", false, "claim both jobs for a rehearsal")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		jobs, err := Due(ctx, db, time.Now(), *force)
		if err != nil {
			return err
		}
		return emit(jobs)
	default:
		return fmt.Errorf("unknown maintenance state command %q", args[0])
	}
}
