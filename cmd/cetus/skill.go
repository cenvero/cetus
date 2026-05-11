package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	claudeSkillURL  = "https://raw.githubusercontent.com/cenvero/cetus/refs/heads/main/.claude/commands/cetus.md"
	claudeSkillFile = "cetus.md"
	skillCacheTTL   = 10 * time.Minute
)

// skillCacheEntry is written to ~/.cenvero-cetus/skill_claude.json
type skillCacheEntry struct {
	SHA256    string    `json:"sha256"`
	CheckedAt time.Time `json:"checked_at"`
}

// BackgroundSkillUpdate silently checks if the remote skill has changed and
// updates ~/.claude/commands/cetus.md if so. Runs as a goroutine. Errors are
// dropped — this must never affect the user's actual command.
func BackgroundSkillUpdate() {
	go func() {
		globalDest, err := globalSkillPath("claude")
		if err != nil {
			return
		}
		// Only auto-update if the global skill is already installed
		if _, err := os.Stat(globalDest); err != nil {
			return
		}
		cache := readSkillCache("claude")
		if cache != nil && time.Since(cache.CheckedAt) < skillCacheTTL {
			return // cache still fresh, no check needed
		}
		data, err := fetchSkillBytes(claudeSkillURL)
		if err != nil {
			return
		}
		hash := sha256Hex(data)
		writeSkillCache("claude", hash)
		if cache != nil && cache.SHA256 == hash {
			return // unchanged
		}
		// Skill changed — update silently
		_ = os.MkdirAll(filepath.Dir(globalDest), 0o755)
		_ = os.WriteFile(globalDest, data, 0o644)
	}()
}

func newSkillCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage Cetus AI skills (Claude Code, more coming)",
		Long: `Install, update, or remove Cetus skills for AI coding assistants.

Skills install to the global Claude commands directory by default (~/.claude/commands/)
so they work in every project without any per-project setup.
Use --local to install to the current project only.

Available:
  claude    Claude Code  (claude.ai/code)

Coming soon:
  codex     OpenAI Codex

Examples:
  cetus skill claude                  install globally (default)
  cetus skill claude --local          install to current project only
  cetus skill claude --delete         remove from global
  cetus skill update claude           force update global skill now
  cetus launch claude                 install globally and launch Claude Code`,
	}
	cmd.AddCommand(newSkillClaudeCommand())
	cmd.AddCommand(newSkillUpdateCommand())
	return cmd
}

func newSkillClaudeCommand() *cobra.Command {
	var local bool
	var delete bool

	cmd := &cobra.Command{
		Use:   "claude",
		Short: "Install the Cetus skill for Claude Code",
		Long: `Downloads the latest Cetus Claude Code skill from GitHub.

Installs to ~/.claude/commands/cetus.md by default (global — works in every project).
Use --local to install to .claude/commands/cetus.md in the current directory only.
Use --delete to remove the global skill.

After installing, type /cetus in Claude Code to activate the assistant.
If Claude Code is already open, restart the session to load the new skill.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if delete {
				return deleteGlobalSkill(cmd, "claude")
			}
			var dest string
			var err error
			if local {
				dest = filepath.Join(".claude", "commands", claudeSkillFile)
			} else {
				dest, err = globalSkillPath("claude")
				if err != nil {
					return err
				}
			}
			if err := downloadAndInstallSkill(cmd, claudeSkillURL, dest, "claude"); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "Type /cetus in Claude Code to activate the assistant.")
			fmt.Fprintln(cmd.OutOrStdout(), "If Claude Code is already open, restart the session to load the new skill.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&local, "local", false, "install to .claude/commands/ in the current project only")
	cmd.Flags().BoolVar(&delete, "delete", false, "remove the global Cetus skill for Claude Code")
	return cmd
}

func newSkillUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Force update an installed global skill to the latest version",
		Long: `Force-downloads and reinstalls a skill regardless of the cache.

Examples:
  cetus skill update claude`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "claude",
		Short: "Force update the global Claude Code skill",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dest, err := globalSkillPath("claude")
			if err != nil {
				return err
			}
			if err := downloadAndInstallSkill(cmd, claudeSkillURL, dest, "claude"); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "If Claude Code is already open, restart the session to load the updated skill.")
			return nil
		},
	})
	return cmd
}

func newLaunchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "launch",
		Short: "Install a Cetus skill globally and open an AI coding assistant",
		Long: `Install the latest Cetus skill for an AI coding assistant and launch it.

Available:
  claude    Install skill globally and launch Claude Code

Examples:
  cetus launch claude`,
	}
	cmd.AddCommand(newLaunchClaudeCommand())
	return cmd
}

func newLaunchClaudeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "claude",
		Short: "Install the Cetus skill globally and open Claude Code",
		Long: `Installs the latest Cetus skill to ~/.claude/commands/cetus.md (global),
creates the directory if it does not exist, then launches Claude Code.

If the claude CLI is not installed, shows where to get it.

Example:
  cetus launch claude`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dest, err := globalSkillPath("claude")
			if err != nil {
				return err
			}
			if err := downloadAndInstallSkill(cmd, claudeSkillURL, dest, "claude"); err != nil {
				return err
			}

			claudePath, err := exec.LookPath("claude")
			if err != nil {
				fmt.Fprintln(cmd.OutOrStdout())
				return fmt.Errorf("claude command not found — install Claude Code from https://claude.ai/code")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "\nLaunching Claude Code...\n")
			launch := exec.Command(claudePath) // #nosec G204 -- launching the claude CLI found on PATH
			launch.Stdin = os.Stdin
			launch.Stdout = os.Stdout
			launch.Stderr = os.Stderr
			return launch.Run()
		},
	}
}

// globalSkillPath returns ~/.claude/commands/<file> for the given tool.
func globalSkillPath(tool string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	switch tool {
	case "claude":
		return filepath.Join(home, ".claude", "commands", claudeSkillFile), nil
	default:
		return "", fmt.Errorf("unknown skill %q", tool)
	}
}

func downloadAndInstallSkill(cmd *cobra.Command, url, dest, tool string) error {
	scope := dest
	fmt.Fprintf(cmd.OutOrStdout(), "Fetching latest Cetus skill...\n")

	data, err := fetchSkillBytes(url)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return fmt.Errorf("write skill: %w", err)
	}

	writeSkillCache(tool, sha256Hex(data))
	fmt.Fprintf(cmd.OutOrStdout(), "Installed → %s\n", scope)
	return nil
}

func fetchSkillBytes(url string) ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url) // #nosec G107 -- URL is a hardcoded constant pointing to the project's own GitHub repo
	if err != nil {
		return nil, fmt.Errorf("fetch skill: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch skill: server returned %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read skill: %w", err)
	}
	return data, nil
}

func deleteGlobalSkill(cmd *cobra.Command, tool string) error {
	dest, err := globalSkillPath(tool)
	if err != nil {
		return err
	}
	if err := os.Remove(dest); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(cmd.OutOrStdout(), "Global skill not found at %s\n", dest)
			return nil
		}
		return fmt.Errorf("remove skill: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed → %s\n", dest)
	return nil
}

func skillCacheFile(tool string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cenvero-cetus", "skill_"+tool+".json")
}

func readSkillCache(tool string) *skillCacheEntry {
	path := skillCacheFile(tool)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is from os.UserHomeDir(), a trusted location
	if err != nil {
		return nil
	}
	var entry skillCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	return &entry
}

func writeSkillCache(tool, hash string) {
	path := skillCacheFile(tool)
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	data, _ := json.Marshal(skillCacheEntry{SHA256: hash, CheckedAt: time.Now()})
	_ = os.WriteFile(path, data, 0o600)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func newUninstallCommand() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall Cetus and remove all Cetus data",
		Long: `Removes Cetus from your system.

If installed via Homebrew, runs: brew uninstall cenvero-cetus
Otherwise, removes the cetus binary and the ~/.cenvero-cetus/ data directory.

Use --yes to skip the confirmation prompt.

Example:
  cetus uninstall
  cetus uninstall --yes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			// Check if Homebrew-managed
			if isHomebrew, _ := isHomebrewInstall(); isHomebrew {
				fmt.Fprintln(out, "Cetus was installed via Homebrew.")
				fmt.Fprintln(out, "Run: brew uninstall cenvero-cetus")
				return nil
			}

			// Find the binary path
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locate cetus binary: %w", err)
			}
			resolved, err := filepath.EvalSymlinks(executable)
			if err != nil {
				resolved = executable
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("find home directory: %w", err)
			}
			dataDir := filepath.Join(home, ".cenvero-cetus")

			fmt.Fprintln(out, "This will remove:")
			fmt.Fprintf(out, "  Binary:    %s\n", resolved)
			fmt.Fprintf(out, "  Data dir:  %s\n", dataDir)

			if !yes {
				fmt.Fprint(out, "\nAre you sure? [y/N] ")
				var answer string
				fmt.Fscan(cmd.InOrStdin(), &answer)
				if answer != "y" && answer != "Y" {
					fmt.Fprintln(out, "Aborted.")
					return nil
				}
			}

			// Remove data directory
			if err := os.RemoveAll(dataDir); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(out, "Warning: could not remove data dir: %v\n", err)
			} else {
				fmt.Fprintf(out, "Removed data dir: %s\n", dataDir)
			}

			// Remove binary last (we're running from it)
			if err := os.Remove(resolved); err != nil {
				return fmt.Errorf("remove binary %s: %w (you may need sudo)", resolved, err)
			}
			fmt.Fprintf(out, "Removed binary: %s\n", resolved)
			fmt.Fprintln(out, "Cetus uninstalled.")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	return cmd
}

// isHomebrewInstall checks whether the running binary lives inside a Homebrew Cellar.
func isHomebrewInstall() (bool, string) {
	executable, err := os.Executable()
	if err != nil {
		return false, ""
	}
	paths := []string{executable}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		paths = append(paths, resolved)
	}
	for _, p := range paths {
		clean := filepath.ToSlash(p)
		if strings.Contains(clean, "/Cellar/cenvero-cetus/") || strings.Contains(clean, "/Cellar/cetus/") {
			return true, p
		}
	}
	return false, ""
}
