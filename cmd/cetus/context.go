package main

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

//go:embed context.txt
var contextPreamble string

func newContextCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "context",
		Short: "Print the full Cetus AI context (seek engine docs, GSAP rules, CLI reference)",
		Long: `Prints the complete Cetus reference as markdown.

The static sections (seek engine, GSAP rules, quality guide, etc.) come from
context.txt embedded in the binary. The CLI commands section is generated
dynamically from the actual command tree — new flags appear automatically.

Examples:
  cetus context
  cetus context > context.md`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), contextPreamble)
			fmt.Fprint(cmd.OutOrStdout(), "\n---\n\n")
			fmt.Fprint(cmd.OutOrStdout(), buildCLIReference(root))
			return nil
		},
	}
}

func buildCLIReference(root *cobra.Command) string {
	var sb strings.Builder
	sb.WriteString("## CLI Commands\n\n")
	for _, cmd := range root.Commands() {
		if cmd.Hidden {
			continue
		}
		writeCommandRef(&sb, cmd, "cetus")
	}
	return sb.String()
}

func writeCommandRef(sb *strings.Builder, cmd *cobra.Command, prefix string) {
	fullName := prefix + " " + cmd.Name()

	sb.WriteString(fullName)
	if cmd.Short != "" {
		sb.WriteString(" — " + cmd.Short)
	}
	sb.WriteString("\n")

	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		line := "  --" + f.Name
		if f.Value.Type() != "bool" {
			line += " " + strings.ToUpper(f.Value.Type())
		}
		line += "\t" + f.Usage
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" && f.DefValue != "[]" {
			line += " (default: " + f.DefValue + ")"
		}
		sb.WriteString(line + "\n")
	})

	if cmd.HasSubCommands() {
		for _, sub := range cmd.Commands() {
			if sub.Hidden {
				continue
			}
			sb.WriteString("\n")
			writeCommandRef(sb, sub, fullName)
		}
	}
	sb.WriteString("\n")
}
