package main

import (
	"fmt"
	"math"
	"os"
	"time"

	"github.com/cenvero/cetus/internal/assets"
	"github.com/cenvero/cetus/internal/browser"
	"github.com/cenvero/cetus/internal/compose"
	"github.com/cenvero/cetus/internal/version"
	"github.com/spf13/cobra"
)

func newSeekCommand() *cobra.Command {
	var output string
	var at string
	var noGPU bool
	var timeoutSeconds int
	var fps int
	var width int
	var height int

	cmd := &cobra.Command{
		Use:   "seek cetus.html --at 5s -o frame.png",
		Short: "Render a single frame from a composition to a PNG",
		Long: `Seek to a specific time in a composition, run the full Cetus seek engine for that frame, and save the result as a lossless PNG.

Useful for previewing what a specific moment looks like without rendering the entire video.

Examples:
  cetus seek cetus.html --at 5s -o frame.png
  cetus seek cetus.html --at 1:30 -o midpoint.png
  cetus seek cetus.html --at 0s -o first.png`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			input := args[0]
			if _, err := os.Stat(input); err != nil {
				return fmt.Errorf("stat input composition: %w", err)
			}

			ts, err := parseTimestamp(at)
			if err != nil {
				return err
			}

			ctx, cancel := renderContext(cmd.Context(), timeoutSeconds)
			defer cancel()

			chromePath, _, err := assets.EnsureAssetsWithProgress(version.Version, func(event assets.ProgressEvent) {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s...\n", event.Message)
			})
			if err != nil {
				return err
			}

			composition, err := compose.Parse(input)
			if err != nil {
				return err
			}

			fpsOverride := 0
			if cmd.Flags().Changed("fps") {
				fpsOverride = fps
			}
			if err := composition.ApplyOverrides(fpsOverride, width, height); err != nil {
				return err
			}

			renderDuration := float64(composition.TotalFrames) / float64(composition.FPS)
			frameIndex := int(math.Round(ts * float64(composition.FPS)))
			if frameIndex >= composition.TotalFrames {
				frameIndex = composition.TotalFrames - 1
			}
			if frameIndex < 0 {
				frameIndex = 0
			}
			actualTime := float64(frameIndex) / float64(composition.FPS)

			fmt.Fprintf(cmd.ErrOrStderr(),
				"Composition %q: %dx%d, %.2fs at %d fps (%d frames)\n",
				composition.ID, composition.Width, composition.Height,
				renderDuration, composition.FPS, composition.TotalFrames,
			)
			fmt.Fprintf(cmd.ErrOrStderr(),
				"Seeking to frame %d (%.3fs)...\n",
				frameIndex, actualTime,
			)

			start := time.Now()
			data, err := browser.SeekFrame(ctx, input, composition, frameIndex, browser.Options{
				ChromePath: chromePath,
				NoGPU:      noGPU,
			})
			if err != nil {
				return err
			}

			if err := os.WriteFile(output, data, 0o600); err != nil {
				return fmt.Errorf("write output: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(),
				"Saved frame %d (%.3fs) → %s in %s\n",
				frameIndex, actualTime, output, time.Since(start).Round(time.Millisecond),
			)
			return nil
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "output PNG file path")
	cmd.Flags().StringVar(&at, "at", "0s", "timestamp to capture (e.g. 5s, 1:30, 01:02:30)")
	cmd.Flags().BoolVar(&noGPU, "no-gpu", false, "disable GPU acceleration")
	cmd.Flags().IntVar(&timeoutSeconds, "timeout", 0, "max time in seconds; 0 disables the deadline")
	cmd.Flags().IntVar(&fps, "fps", 0, "override composition FPS")
	cmd.Flags().IntVar(&width, "width", 0, "override composition width")
	cmd.Flags().IntVar(&height, "height", 0, "override composition height")
	_ = cmd.MarkFlagRequired("output")

	return cmd
}
