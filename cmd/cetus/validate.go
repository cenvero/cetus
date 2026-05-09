package main

import (
	"fmt"

	"github.com/cenvero/cetus/internal/validate"
	"github.com/spf13/cobra"
)

func newValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate cetus.html",
		Short: "Validate a Cetus HTML composition before rendering",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := validate.Check(args[0])
			if err != nil {
				return err
			}

			if result.Composition != nil {
				comp := result.Composition
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"Composition %q: %dx%d, %.2fs at %d fps (%d frames, %d clips)\n",
					comp.ID,
					comp.Width,
					comp.Height,
					comp.Duration,
					comp.FPS,
					comp.TotalFrames,
					len(comp.Clips),
				)
			}

			for _, finding := range result.Findings {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", finding.Severity, finding.Message)
			}

			errors := result.ErrorCount()
			warnings := result.WarningCount()
			if errors > 0 {
				return fmt.Errorf("validation failed with %d error(s) and %d warning(s)", errors, warnings)
			}
			if warnings > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Validation passed with %d warning(s)\n", warnings)
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Validation passed")
			return nil
		},
	}
}
