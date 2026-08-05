package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/croutoncreations/sb-heartbeat/internal/config"
	"github.com/croutoncreations/sb-heartbeat/internal/credentials"
	"github.com/croutoncreations/sb-heartbeat/internal/fileutil"
	"github.com/croutoncreations/sb-heartbeat/internal/heartbeat"
	"github.com/croutoncreations/sb-heartbeat/internal/migration"
	"github.com/croutoncreations/sb-heartbeat/internal/output"
	"github.com/croutoncreations/sb-heartbeat/internal/scheduler"
	"github.com/croutoncreations/sb-heartbeat/internal/security"
	"github.com/spf13/cobra"
)

var Version = "devel"

type Dependencies struct {
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	LookupEnv   func(string) (string, bool)
	RunProjects func(context.Context, []heartbeat.Project, config.Defaults) []heartbeat.Result
	Now         func() time.Time
	Executable  func() (string, error)
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

type preflightProblem struct {
	code    string
	message string
}

type promptOutputError struct{ err error }

func (e *promptOutputError) Error() string { return e.err.Error() }
func (e *promptOutputError) Unwrap() error { return e.err }

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
			if writeErr := output.WriteFailureJSON(dependencies.Stdout, cmdErr.stableCode, cmdErr.message); writeErr != nil {
				fmt.Fprintln(dependencies.Stderr, "Error: write JSON failure output:", writeErr)
				return 3
			}
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
	if dependencies.Executable == nil {
		dependencies.Executable = os.Executable
	}
	if dependencies.RunProjects == nil {
		dependencies.RunProjects = runProjects
	}
	return dependencies
}

func (a *app) rootCommand() *cobra.Command {
	version := currentVersion()
	root := &cobra.Command{
		Use:           "sb-heartbeat",
		Short:         "Run least-privilege Supabase database heartbeats",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().StringVar(&a.configPath, "config", "sb-heartbeat.yaml", "configuration file")
	root.AddCommand(a.runCommand(false), a.runCommand(true), a.initCommand(), a.migrationCommand(), a.installCommand())
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Fprintln(a.dependencies.Stdout, version)
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
	command.Flags().StringVar(&a.outputMode, "output", "", "output format: text or json")
	return command
}

func (a *app) executeChecks(ctx context.Context, selectedProject string, doctor bool) error {
	resolvedConfigPath, err := filepath.Abs(a.configPath)
	if err != nil {
		return &commandError{stableCode: "invalid_configuration", message: "resolve configuration path: " + err.Error()}
	}
	file, err := os.Open(resolvedConfigPath)
	if err != nil {
		return &commandError{stableCode: "invalid_configuration", message: "open configuration " + resolvedConfigPath + ": " + err.Error()}
	}
	cfg, loadErr := config.Load(file)
	closeErr := file.Close()
	if loadErr != nil {
		return &commandError{stableCode: "invalid_configuration", message: "load configuration " + resolvedConfigPath + ": " + loadErr.Error()}
	}
	if closeErr != nil {
		return &commandError{stableCode: "invalid_configuration", message: "close configuration " + resolvedConfigPath + ": " + closeErr.Error()}
	}
	mode := a.outputMode
	if mode == "" {
		mode = cfg.Defaults.Output
	}
	if mode != "text" && mode != "json" {
		return &commandError{stableCode: "invalid_invocation", message: "output must be text or json"}
	}
	a.outputMode = mode

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
		messages := make([]string, 0, len(problems))
		stableCode := "missing_input"
		for _, problem := range problems {
			messages = append(messages, problem.message)
			if problem.code == "credential_rejected" {
				stableCode = problem.code
			}
		}
		return &commandError{stableCode: stableCode, message: strings.Join(messages, "; ")}
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

func (a *app) resolveProjects(configured []config.Project) ([]heartbeat.Project, []preflightProblem) {
	projects := make([]heartbeat.Project, 0, len(configured))
	var problems []preflightProblem
	for _, project := range configured {
		rawURL, ok := a.dependencies.LookupEnv(project.URL.Env)
		if !ok || rawURL == "" {
			problems = append(problems, preflightProblem{code: "missing_input", message: project.Name + ": missing environment variable " + project.URL.Env})
		}
		key, keyOK := a.dependencies.LookupEnv(project.APIKey.Env)
		if !keyOK || key == "" {
			problems = append(problems, preflightProblem{code: "missing_input", message: project.Name + ": missing environment variable " + project.APIKey.Env})
		}
		validatedURL, urlErr := security.ValidateHostedProjectURL(rawURL)
		if ok && rawURL != "" && urlErr != nil {
			problems = append(problems, preflightProblem{code: "missing_input", message: project.Name + ": " + urlErr.Error()})
		}
		if keyOK && key != "" {
			if _, err := credentials.Classify(key); err != nil {
				problems = append(problems, preflightProblem{code: "credential_rejected", message: project.Name + ": " + err.Error()})
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
	var outputPath, projectName, urlEnv, keyEnv, cron, migrationOutput string
	var schedulerName, workflowOutput, workflowConfig, sbHeartbeatVersion string
	command := &cobra.Command{
		Use:   "init",
		Short: "Create a SB Heartbeat configuration",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			var projects []config.Project
			if !nonInteractive {
				reader := bufio.NewReader(a.dependencies.Stdin)
				if projectName == "" {
					projectName = suggestedRepositoryProjectName()
				}
				for {
					project, err := promptProject(reader, a.dependencies.Stdout, projectName, urlEnv, keyEnv)
					if err != nil {
						return promptCommandError(err)
					}
					projects = append(projects, project)
					more, err := promptYesNo(reader, a.dependencies.Stdout, "Add another Supabase project", false)
					if err != nil {
						return promptCommandError(err)
					}
					if !more {
						break
					}
					projectName, urlEnv, keyEnv = "", "", ""
				}
				var err error
				if cron, err = prompt(reader, a.dependencies.Stdout, "Cron schedule", cron); err != nil {
					return promptCommandError(err)
				}
			} else {
				urlEnv, keyEnv = derivedEnvironmentDefaults(projectName, urlEnv, keyEnv)
				if projectName == "" {
					return &commandError{stableCode: "missing_input", message: "project-name is required"}
				}
				projects = append(projects, config.Project{
					Name: projectName, URL: config.EnvReference{Env: urlEnv}, APIKey: config.EnvReference{Env: keyEnv},
				})
			}
			cfg, err := config.NewProjects(projects, cron)
			if err != nil {
				return &commandError{stableCode: "invalid_configuration", message: err.Error()}
			}
			data, err := config.Marshal(cfg)
			if err != nil {
				return &commandError{stableCode: "internal_error", message: err.Error()}
			}
			var workflow []byte
			var migrationData []byte
			if migrationOutput != "" {
				migrationData = []byte(migration.InstallSQL())
			}
			if schedulerName != "" {
				if schedulerName != "github" {
					return &commandError{stableCode: "invalid_invocation", message: "scheduler must be github"}
				}
				effectiveConfigPath, pathErr := resolveWorkflowConfigPath(workflowConfig, outputPath, workflowOutput)
				if pathErr != nil {
					return &commandError{stableCode: "invalid_invocation", message: pathErr.Error()}
				}
				workflow, err = scheduler.GitHub(cfg, sbHeartbeatVersion, effectiveConfigPath)
				if err != nil {
					return &commandError{stableCode: "invalid_invocation", message: err.Error()}
				}
			}
			targets := []string{outputPath}
			if migrationData != nil {
				targets = append(targets, migrationOutput)
			}
			if workflow != nil {
				targets = append(targets, workflowOutput)
			}
			if err := validateDistinctOutputPaths(targets); err != nil {
				return &commandError{stableCode: "invalid_invocation", message: err.Error()}
			}
			for _, target := range targets {
				if err := fileutil.CheckTarget(target, force); err != nil {
					return generatedFileCommandError(err, "")
				}
			}
			if err := fileutil.WriteAtomic(outputPath, data, 0o644, force); err != nil {
				return generatedFileCommandError(err, "")
			}
			fmt.Fprintln(a.dependencies.Stdout, "Created", outputPath)
			if migrationData != nil {
				if err := fileutil.WriteAtomic(migrationOutput, migrationData, 0o644, force); err != nil {
					return generatedFileCommandError(err, "configuration was created, but migration creation failed: ")
				}
				fmt.Fprintln(a.dependencies.Stdout, "Created", migrationOutput)
			}
			if workflow != nil {
				if err := fileutil.WriteAtomic(workflowOutput, workflow, 0o644, force); err != nil {
					return generatedFileCommandError(err, "earlier files were created, but workflow creation failed: ")
				}
				fmt.Fprintln(a.dependencies.Stdout, "Created", workflowOutput)
			}
			if _, err := io.WriteString(a.dependencies.Stdout, githubBindingSummary(cfg.Projects)); err != nil {
				return &commandError{stableCode: "internal_error", message: "files were created, but write GitHub binding guidance: " + err.Error()}
			}
			return nil
		},
	}
	command.Flags().BoolVar(&nonInteractive, "non-interactive", false, "require all setup values as flags")
	command.Flags().BoolVar(&force, "force", false, "replace the exact output file")
	command.Flags().StringVar(&outputPath, "output-path", "sb-heartbeat.yaml", "configuration output path")
	command.Flags().StringVar(&projectName, "project-name", "", "project name")
	command.Flags().StringVar(&urlEnv, "url-env", "", "environment variable containing the project URL")
	command.Flags().StringVar(&keyEnv, "api-key-env", "", "environment variable containing the low-privilege API key")
	command.Flags().StringVar(&cron, "cron", config.DefaultCron, "scheduler cron expression")
	command.Flags().StringVar(&migrationOutput, "migration-output", "", "also write the install migration to this exact path")
	command.Flags().StringVar(&schedulerName, "scheduler", "", "also generate a scheduler: github")
	command.Flags().StringVar(&workflowOutput, "workflow-output", ".github/workflows/sb-heartbeat.yml", "GitHub workflow output path")
	command.Flags().StringVar(&workflowConfig, "workflow-config", "", "repository-relative config path used by GitHub Actions (defaults to the generated config path)")
	command.Flags().StringVar(&sbHeartbeatVersion, "sb-heartbeat-version", currentVersion(), "exact SB Heartbeat release tag for generated automation")
	return command
}

func promptProject(reader *bufio.Reader, writer io.Writer, projectName, urlEnv, keyEnv string) (config.Project, error) {
	var err error
	if projectName, err = prompt(reader, writer, "Project name", projectName); err != nil {
		return config.Project{}, err
	}
	urlEnv, keyEnv = derivedEnvironmentDefaults(projectName, urlEnv, keyEnv)
	if _, err := fmt.Fprintf(writer, "Bindings for %s (press Enter to accept or type an existing name):\n", projectName); err != nil {
		return config.Project{}, &promptOutputError{err: fmt.Errorf("write project binding guidance: %w", err)}
	}
	if _, err := fmt.Fprintln(writer, "  GitHub variable:", urlEnv); err != nil {
		return config.Project{}, &promptOutputError{err: fmt.Errorf("write project URL binding guidance: %w", err)}
	}
	if _, err := fmt.Fprintln(writer, "  GitHub secret:", keyEnv); err != nil {
		return config.Project{}, &promptOutputError{err: fmt.Errorf("write project API-key binding guidance: %w", err)}
	}
	if urlEnv, err = prompt(reader, writer, "Project URL environment variable", urlEnv); err != nil {
		return config.Project{}, err
	}
	if keyEnv, err = prompt(reader, writer, "API key environment variable", keyEnv); err != nil {
		return config.Project{}, err
	}
	return config.Project{
		Name: projectName, URL: config.EnvReference{Env: urlEnv}, APIKey: config.EnvReference{Env: keyEnv},
	}, nil
}

func promptCommandError(err error) *commandError {
	var outputErr *promptOutputError
	if errors.As(err, &outputErr) {
		return &commandError{stableCode: "internal_error", message: err.Error()}
	}
	return &commandError{stableCode: "missing_input", message: err.Error()}
}

func githubBindingSummary(projects []config.Project) string {
	var summary strings.Builder
	summary.WriteString("\nGitHub repository bindings (values are not stored by SB Heartbeat):\n")
	for _, project := range projects {
		fmt.Fprintf(&summary, "%s:\n  GitHub variable: %s\n  GitHub secret: %s\n", project.Name, project.URL.Env, project.APIKey.Env)
	}
	return summary.String()
}

func suggestedRepositoryProjectName() string {
	directory, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, ".git")); err == nil {
			return normalizeProjectSuggestion(filepath.Base(directory))
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
		directory = parent
	}
}

func normalizeProjectSuggestion(name string) string {
	var normalized strings.Builder
	previousSeparator := false
	for _, character := range strings.ToLower(name) {
		valid := character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
		if valid {
			normalized.WriteRune(character)
			previousSeparator = false
		} else if !previousSeparator {
			normalized.WriteByte('-')
			previousSeparator = true
		}
	}
	result := strings.Trim(normalized.String(), "-")
	if result == "" {
		return ""
	}
	if result[0] < 'a' || result[0] > 'z' {
		result = "project-" + result
	}
	if len(result) > 63 {
		result = strings.TrimRight(result[:63], "-")
	}
	return result
}

func derivedEnvironmentDefaults(projectName, urlEnv, keyEnv string) (string, string) {
	suggestedURL, suggestedKey := config.SuggestedEnvironmentNames(projectName)
	if urlEnv == "" {
		urlEnv = suggestedURL
	}
	if keyEnv == "" {
		keyEnv = suggestedKey
	}
	return urlEnv, keyEnv
}

func (a *app) installCommand() *cobra.Command {
	parent := &cobra.Command{Use: "install", Short: "Generate scheduler integrations"}
	var outputPath, version, workflowConfig string
	var force bool
	github := &cobra.Command{
		Use:   "github",
		Short: "Generate a GitHub Actions workflow",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			resolvedConfigPath, err := filepath.Abs(a.configPath)
			if err != nil {
				return &commandError{stableCode: "invalid_configuration", message: "resolve configuration path: " + err.Error()}
			}
			file, err := os.Open(resolvedConfigPath)
			if err != nil {
				return &commandError{stableCode: "invalid_configuration", message: "open configuration " + resolvedConfigPath + ": " + err.Error()}
			}
			cfg, loadErr := config.Load(file)
			closeErr := file.Close()
			if loadErr != nil {
				return &commandError{stableCode: "invalid_configuration", message: "load configuration " + resolvedConfigPath + ": " + loadErr.Error()}
			}
			if closeErr != nil {
				return &commandError{stableCode: "invalid_configuration", message: "close configuration " + resolvedConfigPath + ": " + closeErr.Error()}
			}
			effectiveConfigPath, pathErr := resolveWorkflowConfigPath(workflowConfig, a.configPath, outputPath)
			if pathErr != nil {
				return &commandError{stableCode: "invalid_invocation", message: pathErr.Error()}
			}
			workflow, err := scheduler.GitHub(cfg, version, effectiveConfigPath)
			if err != nil {
				return &commandError{stableCode: "invalid_invocation", message: err.Error()}
			}
			if err := fileutil.WriteAtomic(outputPath, workflow, 0o644, force); err != nil {
				return generatedFileCommandError(err, "")
			}
			fmt.Fprintln(a.dependencies.Stdout, "Created", outputPath)
			return nil
		},
	}
	github.Flags().StringVar(&outputPath, "output-path", ".github/workflows/sb-heartbeat.yml", "workflow output path")
	github.Flags().StringVar(&version, "sb-heartbeat-version", currentVersion(), "exact SB Heartbeat release tag")
	github.Flags().StringVar(&workflowConfig, "workflow-config", "", "repository-relative config path used by GitHub Actions (defaults to --config)")
	github.Flags().BoolVar(&force, "force", false, "replace the exact output file")

	var cronBinaryPath, cronLogPath string
	cron := &cobra.Command{
		Use:   "cron",
		Short: "Print a suggested local crontab entry without installing it",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			resolvedConfigPath, err := filepath.Abs(a.configPath)
			if err != nil {
				return &commandError{stableCode: "invalid_configuration", message: "resolve configuration path: " + err.Error()}
			}
			file, err := os.Open(resolvedConfigPath)
			if err != nil {
				return &commandError{stableCode: "invalid_configuration", message: "open configuration " + resolvedConfigPath + ": " + err.Error()}
			}
			cfg, loadErr := config.Load(file)
			closeErr := file.Close()
			if loadErr != nil {
				return &commandError{stableCode: "invalid_configuration", message: "load configuration " + resolvedConfigPath + ": " + loadErr.Error()}
			}
			if closeErr != nil {
				return &commandError{stableCode: "invalid_configuration", message: "close configuration " + resolvedConfigPath + ": " + closeErr.Error()}
			}
			binaryPath := cronBinaryPath
			if binaryPath == "" {
				binaryPath, err = a.dependencies.Executable()
				if err != nil {
					return &commandError{stableCode: "internal_error", message: "resolve executable path: " + err.Error()}
				}
				binaryPath, err = filepath.Abs(binaryPath)
				if err != nil {
					return &commandError{stableCode: "internal_error", message: "resolve absolute executable path: " + err.Error()}
				}
			}
			entry, err := scheduler.LocalCron(cfg, binaryPath, resolvedConfigPath, cronLogPath)
			if err != nil {
				return &commandError{stableCode: "invalid_invocation", message: err.Error()}
			}
			if _, err := io.WriteString(a.dependencies.Stdout, entry); err != nil {
				return &commandError{stableCode: "internal_error", message: "write cron suggestion: " + err.Error()}
			}
			return nil
		},
	}
	cron.Flags().StringVar(&cronBinaryPath, "binary-path", "", "absolute path to the sb-heartbeat binary")
	cron.Flags().StringVar(&cronLogPath, "log-path", "", "optional absolute path for appended stdout and stderr")
	parent.AddCommand(github, cron)
	return parent
}

func prompt(reader *bufio.Reader, writer io.Writer, label, defaultValue string) (string, error) {
	if defaultValue == "" {
		if _, err := fmt.Fprintf(writer, "%s: ", label); err != nil {
			return "", &promptOutputError{err: fmt.Errorf("write %s prompt: %w", strings.ToLower(label), err)}
		}
	} else {
		if _, err := fmt.Fprintf(writer, "%s [%s]: ", label, defaultValue); err != nil {
			return "", &promptOutputError{err: fmt.Errorf("write %s prompt: %w", strings.ToLower(label), err)}
		}
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

func promptYesNo(reader *bufio.Reader, writer io.Writer, label string, defaultValue bool) (bool, error) {
	suffix := "[y/N]"
	if defaultValue {
		suffix = "[Y/n]"
	}
	if _, err := fmt.Fprintf(writer, "%s %s: ", label, suffix); err != nil {
		return false, &promptOutputError{err: fmt.Errorf("write %s prompt: %w", strings.ToLower(label), err)}
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read %s: %w", strings.ToLower(label), err)
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return defaultValue, nil
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be yes or no", strings.ToLower(label))
	}
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
				return generatedFileCommandError(err, "")
			}
			fmt.Fprintln(a.dependencies.Stdout, "Created", outputPath)
			return nil
		},
	}
	command.Flags().StringVar(&outputPath, "output", "", "write SQL to this path")
	command.Flags().BoolVar(&force, "force", false, "replace the exact output file")
	return command
}

func generatedFileCommandError(err error, prefix string) *commandError {
	stableCode := "internal_error"
	if fileutil.IsTargetError(err) {
		stableCode = "invalid_invocation"
	}
	return &commandError{stableCode: stableCode, message: prefix + err.Error()}
}

func validateDistinctOutputPaths(paths []string) error {
	seen := make(map[string]string, len(paths))
	for _, outputPath := range paths {
		resolved, err := filepath.Abs(outputPath)
		if err != nil {
			return fmt.Errorf("resolve output path %q: %w", outputPath, err)
		}
		if previous, exists := seen[resolved]; exists {
			return fmt.Errorf("generated outputs must be different files: %s and %s", previous, outputPath)
		}
		seen[resolved] = outputPath
	}
	return nil
}

func resolveWorkflowConfigPath(explicitPath, configPath, workflowPath string) (string, error) {
	if explicitPath != "" {
		return filepath.ToSlash(explicitPath), nil
	}
	if !filepath.IsAbs(configPath) {
		return filepath.ToSlash(filepath.Clean(configPath)), nil
	}
	configAbsolute, err := filepath.Abs(configPath)
	if err != nil {
		return "", fmt.Errorf("resolve generated configuration path: %w", err)
	}
	workflowAbsolute, err := filepath.Abs(workflowPath)
	if err != nil {
		return "", fmt.Errorf("resolve generated workflow path: %w", err)
	}
	workflowDirectory := filepath.Dir(workflowAbsolute)
	if filepath.Base(workflowDirectory) != "workflows" || filepath.Base(filepath.Dir(workflowDirectory)) != ".github" {
		return "", errors.New("cannot infer repository root from workflow output; supply --workflow-config")
	}
	repositoryRoot := filepath.Dir(filepath.Dir(workflowDirectory))
	relative, err := filepath.Rel(repositoryRoot, configAbsolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("configuration is outside the generated workflow repository; supply --workflow-config")
	}
	return filepath.ToSlash(relative), nil
}

func currentVersion() string {
	moduleVersion := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		moduleVersion = info.Main.Version
	}
	return resolveVersion(Version, moduleVersion)
}

func resolveVersion(linkerVersion, moduleVersion string) string {
	if linkerVersion != "" && linkerVersion != "devel" {
		return linkerVersion
	}
	if moduleVersion != "" && moduleVersion != "(devel)" {
		return moduleVersion
	}
	return "devel"
}
