package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jfox85/supawake/internal/config"
	"github.com/jfox85/supawake/internal/credentials"
	"github.com/jfox85/supawake/internal/fileutil"
	"github.com/jfox85/supawake/internal/heartbeat"
	"github.com/jfox85/supawake/internal/migration"
	"github.com/jfox85/supawake/internal/output"
	"github.com/jfox85/supawake/internal/security"
	"github.com/spf13/cobra"
)

const Version = "0.1.0-dev"

type Dependencies struct {
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	LookupEnv   func(string) (string, bool)
	RunProjects func(context.Context, []heartbeat.Project, config.Defaults) []heartbeat.Result
	Now         func() time.Time
}

type app struct {
	dependencies Dependencies
	configPath   string
	outputMode   string
	exitCode     int
}

type commandError struct {
	stableCode string
	message    string
}

func (e *commandError) Error() string { return e.message }

func Execute(ctx context.Context, args []string, dependencies Dependencies) int {
	dependencies = withDefaults(dependencies)
	a := &app{dependencies: dependencies}
	root := a.rootCommand()
	root.SetArgs(args)
	root.SetIn(dependencies.Stdin)
	root.SetOut(dependencies.Stdout)
	root.SetErr(dependencies.Stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		var cmdErr *commandError
		if !errors.As(err, &cmdErr) {
			cmdErr = &commandError{stableCode: "invalid_invocation", message: err.Error()}
		}
		if a.outputMode == "json" {
			_ = output.WriteFailureJSON(dependencies.Stdout, cmdErr.stableCode, cmdErr.message)
		} else {
			fmt.Fprintln(dependencies.Stderr, "Error:", cmdErr.message)
		}
		if cmdErr.stableCode == "internal_error" {
			return 3
		}
		return 2
	}
	return a.exitCode
}

func withDefaults(dependencies Dependencies) Dependencies {
	if dependencies.Stdin == nil {
		dependencies.Stdin = os.Stdin
	}
	if dependencies.Stdout == nil {
		dependencies.Stdout = os.Stdout
	}
	if dependencies.Stderr == nil {
		dependencies.Stderr = os.Stderr
	}
	if dependencies.LookupEnv == nil {
		dependencies.LookupEnv = os.LookupEnv
	}
	if dependencies.Now == nil {
		dependencies.Now = time.Now
	}
	if dependencies.RunProjects == nil {
		dependencies.RunProjects = runProjects
	}
	return dependencies
}

func (a *app) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "supawake",
		Short:         "Run least-privilege Supabase database heartbeats",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().StringVar(&a.configPath, "config", "supawake.yaml", "configuration file")
	root.PersistentFlags().StringVar(&a.outputMode, "output", "", "output format: text or json")
	root.AddCommand(a.runCommand(false), a.runCommand(true), a.initCommand(), a.migrationCommand())
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Fprintln(a.dependencies.Stdout, Version)
		},
	})
	return root
}

func (a *app) runCommand(doctor bool) *cobra.Command {
	name := "run"
	short := "Run configured heartbeats"
	if doctor {
		name, short = "doctor", "Validate configuration and run non-mutating heartbeat diagnostics"
	}
	var selectedProject string
	command := &cobra.Command{
		Use:   name,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.executeChecks(cmd.Context(), selectedProject, doctor)
		},
	}
	command.Flags().StringVar(&selectedProject, "project", "", "run only the named project")
	return command
}

func (a *app) executeChecks(ctx context.Context, selectedProject string, doctor bool) error {
	file, err := os.Open(a.configPath)
	if err != nil {
		return &commandError{stableCode: "invalid_configuration", message: "open configuration: " + err.Error()}
	}
	cfg, loadErr := config.Load(file)
	closeErr := file.Close()
	if loadErr != nil {
		return &commandError{stableCode: "invalid_configuration", message: loadErr.Error()}
	}
	if closeErr != nil {
		return &commandError{stableCode: "invalid_configuration", message: "close configuration: " + closeErr.Error()}
	}

	configured := cfg.Projects
	if selectedProject != "" {
		configured = nil
		for _, project := range cfg.Projects {
			if project.Name == selectedProject {
				configured = append(configured, project)
				break
			}
		}
		if len(configured) == 0 {
			return &commandError{stableCode: "missing_input", message: "project is not configured: " + selectedProject}
		}
	}

	projects, problems := a.resolveProjects(configured)
	if len(problems) > 0 {
		return &commandError{stableCode: "missing_input", message: strings.Join(problems, "; ")}
	}
	mode := a.outputMode
	if mode == "" {
		mode = cfg.Defaults.Output
	}
	if mode != "text" && mode != "json" {
		return &commandError{stableCode: "invalid_invocation", message: "output must be text or json"}
	}

	started := a.dependencies.Now()
	results := a.dependencies.RunProjects(ctx, projects, cfg.Defaults)
	finished := a.dependencies.Now()
	if len(results) != len(projects) {
		return &commandError{stableCode: "internal_error", message: "heartbeat runner returned an incomplete result set"}
	}
	if mode == "json" {
		if err := output.WriteJSON(a.dependencies.Stdout, output.Run{StartedAt: started, FinishedAt: finished, Results: results}); err != nil {
			return &commandError{stableCode: "internal_error", message: "write JSON output: " + err.Error()}
		}
	} else if err := output.WriteText(a.dependencies.Stdout, results); err != nil {
		return &commandError{stableCode: "internal_error", message: "write text output: " + err.Error()}
	}
	if doctor && mode == "text" {
		for _, result := range results {
			if result.Status == heartbeat.Healthy {
				continue
			}
			fmt.Fprintf(a.dependencies.Stdout, "  %s: %s\n", result.Name, doctorGuidance(result.Status))
		}
	}
	a.exitCode = heartbeat.ExitCode(results)
	return nil
}

func doctorGuidance(status heartbeat.Status) string {
	switch status {
	case heartbeat.DatabasePermissionDenied:
		return "possible causes include schema exposure/USAGE, table or column grants, or RLS; the low-privilege request cannot distinguish them"
	case heartbeat.APIAuthenticationFailed, heartbeat.CredentialRejected:
		return "check that the configured value is a current publishable or legacy anon key"
	case heartbeat.NoMatchingRow:
		return "check that the guarded migration and fixed heartbeat row were applied"
	case heartbeat.ProjectPaused:
		return "the project must be resumed from Supabase before it can answer requests"
	default:
		return "inspect the stable status code and network or project configuration; no mutation probe was performed"
	}
}

func (a *app) resolveProjects(configured []config.Project) ([]heartbeat.Project, []string) {
	projects := make([]heartbeat.Project, 0, len(configured))
	var problems []string
	for _, project := range configured {
		rawURL, ok := a.dependencies.LookupEnv(project.URL.Env)
		if !ok || rawURL == "" {
			problems = append(problems, project.Name+": missing environment variable "+project.URL.Env)
		}
		key, keyOK := a.dependencies.LookupEnv(project.APIKey.Env)
		if !keyOK || key == "" {
			problems = append(problems, project.Name+": missing environment variable "+project.APIKey.Env)
		}
		validatedURL, urlErr := security.ValidateHostedProjectURL(rawURL)
		if ok && rawURL != "" && urlErr != nil {
			problems = append(problems, project.Name+": "+urlErr.Error())
		}
		if keyOK && key != "" {
			if _, err := credentials.Classify(key); err != nil {
				problems = append(problems, project.Name+": "+err.Error())
			}
		}
		if ok && rawURL != "" && urlErr == nil && keyOK && key != "" {
			if _, err := credentials.Classify(key); err == nil {
				projects = append(projects, heartbeat.Project{Name: project.Name, BaseURL: validatedURL, APIKey: key})
			}
		}
	}
	return projects, problems
}

func runProjects(ctx context.Context, projects []heartbeat.Project, defaults config.Defaults) []heartbeat.Result {
	runner := heartbeat.NewRunner(heartbeat.Options{
		Timeout:      &defaults.Timeout,
		Retries:      &defaults.Retries,
		RetryBackoff: &defaults.RetryBackoff,
		Concurrency:  &defaults.Concurrency,
	})
	return runner.RunAll(ctx, projects)
}

func (a *app) initCommand() *cobra.Command {
	var nonInteractive, force bool
	var outputPath, projectName, urlEnv, keyEnv, cron string
	command := &cobra.Command{
		Use:   "init",
		Short: "Create a Supawake configuration",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if !nonInteractive {
				reader := bufio.NewReader(a.dependencies.Stdin)
				var err error
				if projectName, err = prompt(reader, a.dependencies.Stdout, "Project name", projectName); err != nil {
					return &commandError{stableCode: "missing_input", message: err.Error()}
				}
				if urlEnv, err = prompt(reader, a.dependencies.Stdout, "Project URL environment variable", urlEnv); err != nil {
					return &commandError{stableCode: "missing_input", message: err.Error()}
				}
				if keyEnv, err = prompt(reader, a.dependencies.Stdout, "API key environment variable", keyEnv); err != nil {
					return &commandError{stableCode: "missing_input", message: err.Error()}
				}
				if cron, err = prompt(reader, a.dependencies.Stdout, "Cron schedule", cron); err != nil {
					return &commandError{stableCode: "missing_input", message: err.Error()}
				}
			}
			if projectName == "" || urlEnv == "" || keyEnv == "" {
				return &commandError{stableCode: "missing_input", message: "project-name, url-env, and api-key-env are required"}
			}
			cfg, err := config.New(config.Project{
				Name:   projectName,
				URL:    config.EnvReference{Env: urlEnv},
				APIKey: config.EnvReference{Env: keyEnv},
			}, cron)
			if err != nil {
				return &commandError{stableCode: "invalid_configuration", message: err.Error()}
			}
			data, err := config.Marshal(cfg)
			if err != nil {
				return &commandError{stableCode: "internal_error", message: err.Error()}
			}
			if err := fileutil.WriteAtomic(outputPath, data, 0o644, force); err != nil {
				return &commandError{stableCode: "invalid_invocation", message: err.Error()}
			}
			fmt.Fprintln(a.dependencies.Stdout, "Created", outputPath)
			return nil
		},
	}
	command.Flags().BoolVar(&nonInteractive, "non-interactive", false, "require all setup values as flags")
	command.Flags().BoolVar(&force, "force", false, "replace the exact output file")
	command.Flags().StringVar(&outputPath, "output-path", "supawake.yaml", "configuration output path")
	command.Flags().StringVar(&projectName, "project-name", "", "project name")
	command.Flags().StringVar(&urlEnv, "url-env", "", "environment variable containing the project URL")
	command.Flags().StringVar(&keyEnv, "api-key-env", "", "environment variable containing the low-privilege API key")
	command.Flags().StringVar(&cron, "cron", config.DefaultCron, "scheduler cron expression")
	return command
}

func prompt(reader *bufio.Reader, writer io.Writer, label, defaultValue string) (string, error) {
	if defaultValue == "" {
		fmt.Fprintf(writer, "%s: ", label)
	} else {
		fmt.Fprintf(writer, "%s [%s]: ", label, defaultValue)
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read %s: %w", strings.ToLower(label), err)
	}
	value := strings.TrimSpace(line)
	if value == "" {
		value = defaultValue
	}
	if value == "" {
		return "", fmt.Errorf("%s is required", strings.ToLower(label))
	}
	return value, nil
}

func (a *app) migrationCommand() *cobra.Command {
	parent := &cobra.Command{Use: "migration", Short: "Generate heartbeat SQL"}
	parent.AddCommand(a.migrationLeaf("install", migration.InstallSQL), a.migrationLeaf("uninstall", migration.UninstallSQL))
	return parent
}

func (a *app) migrationLeaf(name string, generate func() string) *cobra.Command {
	var outputPath string
	var force bool
	command := &cobra.Command{
		Use:   name,
		Short: "Generate the " + name + " migration",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			data := []byte(generate())
			if outputPath == "" {
				_, err := a.dependencies.Stdout.Write(data)
				if err != nil {
					return &commandError{stableCode: "internal_error", message: "write SQL output: " + err.Error()}
				}
				return nil
			}
			if err := fileutil.WriteAtomic(outputPath, data, 0o644, force); err != nil {
				return &commandError{stableCode: "invalid_invocation", message: err.Error()}
			}
			fmt.Fprintln(a.dependencies.Stdout, "Created", outputPath)
			return nil
		},
	}
	command.Flags().StringVar(&outputPath, "output", "", "write SQL to this path")
	command.Flags().BoolVar(&force, "force", false, "replace the exact output file")
	return command
}
