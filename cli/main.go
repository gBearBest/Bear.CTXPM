package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/engine"
)

type commandError struct {
	message string
	code    int
}

type stringListFlag []string

func (s *stringListFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringListFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (e *commandError) Error() string {
	return e.message
}

func failf(code int, format string, args ...any) error {
	return &commandError{
		message: fmt.Sprintf(format, args...),
		code:    code,
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		var cmdErr *commandError
		if errors.As(err, &cmdErr) {
			fmt.Fprintln(os.Stderr, cmdErr.message)
			os.Exit(cmdErr.code)
		}
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printHelp(os.Stdout)
		return nil
	}

	root, err := os.Getwd()
	if err != nil {
		return err
	}
	app := engine.New(root)

	switch args[0] {
	case "help", "--help", "-h":
		printHelp(os.Stdout)
		return nil
	case "version", "--version", "-v":
		return runVersion()
	case "init":
		return runInit(app, args[1:])
	case "add":
		return runAdd(app, args[1:])
	case "list":
		return runList(app, args[1:])
	case "validate":
		return runValidate(app, args[1:])
	case "install":
		return runInstall(app, args[1:])
	case "detect":
		return runDetect(app, args[1:])
	case "migrate":
		return runMigrate(app, args[1:])
	case "check-updates":
		return runCheckUpdates(app, args[1:])
	case "update":
		return runUpdate(app, args[1:])
	case "remove":
		return runRemove(app, args[1:])
	default:
		return failf(2, "unknown command %q", args[0])
	}
}

func runInit(app *engine.App, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	agent := fs.String("agent", "", "Primary agent profile (defaults to detected agent or generic)")
	projectName := fs.String("project-name", "", "Override project name")
	force := fs.Bool("force", false, "Overwrite missing managed files when manifest already exists")
	dryRun := fs.Bool("dry-run", false, "Report changes without writing files")
	if err := fs.Parse(args); err != nil {
		return err
	}

	result, err := app.Init(engine.InitOptions{
		Agent:       *agent,
		ProjectName: *projectName,
		Force:       *force,
		DryRun:      *dryRun,
	})
	if err != nil {
		return err
	}
	fmt.Print(result.Text())
	return nil
}

func runAdd(app *engine.App, args []string) error {
	args = reorderArgs(args, map[string]bool{
		"--type":        true,
		"--name":        true,
		"--layout":      true,
		"--source-type": true,
		"--source-path": true,
		"--target-path": true,
		"--ref":         true,
		"--entry":       true,
		"--file":        true,
	})
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	resourceType := fs.String("type", "", "Resource type: skill|rule|spec|prompt|mcp")
	name := fs.String("name", "", "Override resource name")
	layout := fs.String("layout", "", "Resource layout: file|dir")
	sourceType := fs.String("source-type", "", "Source type: git|url|archive")
	sourcePath := fs.String("source-path", "", "Path inside a git repository when it cannot be inferred from the URL")
	targetPath := fs.String("target-path", "", "Override canonical install path")
	ref := fs.String("ref", "", "Override git ref")
	entry := fs.String("entry", "", "Entry filename relative to the resource root")
	var files stringListFlag
	fs.Var(&files, "file", "Relative file path for multi-file URL resources; repeat to add more files")
	jsonOutput := fs.Bool("json", false, "Emit JSON output")
	dryRun := fs.Bool("dry-run", false, "Resolve and report without changing files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *resourceType == "" {
		return failf(2, "--type is required")
	}
	if fs.NArg() != 1 {
		return failf(2, "usage: ctxpm add <source-url> --type <type> [options]")
	}

	result, err := app.Add(context.Background(), engine.AddOptions{
		SourceURL:  fs.Arg(0),
		Type:       *resourceType,
		Name:       *name,
		Layout:     *layout,
		SourceType: *sourceType,
		SourcePath: *sourcePath,
		TargetPath: *targetPath,
		Ref:        *ref,
		Entry:      *entry,
		Files:      append([]string(nil), files...),
		DryRun:     *dryRun,
	})
	if err != nil {
		return err
	}
	return printMaybeJSON(result, *jsonOutput)
}

func runList(app *engine.App, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	resourceType := fs.String("type", "", "Filter by resource type")
	kind := fs.String("kind", "", "Filter by kind: dependency|package")
	jsonOutput := fs.Bool("json", false, "Emit JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	result, err := app.List(engine.ListOptions{
		Type: *resourceType,
		Kind: *kind,
	})
	if err != nil {
		return err
	}
	return printMaybeJSON(result, *jsonOutput)
}

func runValidate(app *engine.App, args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOutput := fs.Bool("json", false, "Emit JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	result, err := app.Validate()
	if err != nil {
		return err
	}
	return printMaybeJSON(result, *jsonOutput)
}

func runInstall(app *engine.App, args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	resourceType := fs.String("type", "", "Only install resources of this type")
	only := fs.String("only", "", "Only install or repair a single resource by name")
	dryRun := fs.Bool("dry-run", false, "Report the work without writing files")
	jsonOutput := fs.Bool("json", false, "Emit JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	result, err := app.Install(context.Background(), engine.InstallOptions{
		Type:   *resourceType,
		Only:   *only,
		DryRun: *dryRun,
	})
	if err != nil {
		return err
	}
	return printMaybeJSON(result, *jsonOutput)
}

func runDetect(app *engine.App, args []string) error {
	fs := flag.NewFlagSet("detect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOutput := fs.Bool("json", false, "Emit JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	result, err := app.Detect()
	if err != nil {
		return err
	}
	return printMaybeJSON(result, *jsonOutput)
}

func runMigrate(app *engine.App, args []string) error {
	args = reorderArgs(args, map[string]bool{
		"--path": true,
	})
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	all := fs.Bool("all", false, "Migrate every detected candidate")
	dryRun := fs.Bool("dry-run", false, "Report the work without writing files")
	jsonOutput := fs.Bool("json", false, "Emit JSON output")
	var paths stringListFlag
	fs.Var(&paths, "path", "Original path or resource name of a detected candidate; repeat to migrate multiple resources")
	if err := fs.Parse(args); err != nil {
		return err
	}

	result, err := app.Migrate(engine.MigrateOptions{
		Paths:  append([]string(nil), append([]string(paths), fs.Args()...)...),
		All:    *all,
		DryRun: *dryRun,
	})
	if err != nil {
		return err
	}
	return printMaybeJSON(result, *jsonOutput)
}

func runCheckUpdates(app *engine.App, args []string) error {
	fs := flag.NewFlagSet("check-updates", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	force := fs.Bool("force", false, "Ignore the configured interval and check now")
	jsonOutput := fs.Bool("json", false, "Emit JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	result, err := app.CheckUpdates(context.Background(), engine.CheckUpdatesOptions{
		Force: *force,
	})
	if err != nil {
		return err
	}
	return printMaybeJSON(result, *jsonOutput)
}

func runUpdate(app *engine.App, args []string) error {
	args = reorderArgs(args, map[string]bool{})
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	all := fs.Bool("all", false, "Update every dependency with an available update")
	dryRun := fs.Bool("dry-run", false, "Resolve and report without changing files")
	jsonOutput := fs.Bool("json", false, "Emit JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	result, err := app.Update(context.Background(), engine.UpdateOptions{
		Names:  fs.Args(),
		All:    *all,
		DryRun: *dryRun,
	})
	if err != nil {
		return err
	}
	return printMaybeJSON(result, *jsonOutput)
}

func runRemove(app *engine.App, args []string) error {
	args = reorderArgs(args, map[string]bool{})
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	deleteFiles := fs.Bool("delete-files", false, "Delete canonical files and compatibility symlinks")
	keepFiles := fs.Bool("keep-files", false, "Leave canonical files on disk")
	jsonOutput := fs.Bool("json", false, "Emit JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return failf(2, "usage: ctxpm remove <name> [--delete-files|--keep-files]")
	}
	if *deleteFiles && *keepFiles {
		return failf(2, "--delete-files and --keep-files are mutually exclusive")
	}

	result, err := app.Remove(engine.RemoveOptions{
		Name:        fs.Arg(0),
		DeleteFiles: *deleteFiles,
	})
	if err != nil {
		return err
	}
	return printMaybeJSON(result, *jsonOutput)
}

func printMaybeJSON(value any, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}

	type textRenderer interface {
		Text() string
	}
	if renderer, ok := value.(textRenderer); ok {
		fmt.Print(renderer.Text())
		return nil
	}

	return failf(2, "result does not support text rendering")
}

func printHelp(w *os.File) {
	lines := []string{
		"ctxpm - Bear.CTXPM reference CLI",
		"",
		"Usage:",
		"  ctxpm <command> [options]",
		"",
		"Commands:",
		"  version        Print CLI version information",
		"  init           Initialize the current project as a Bear.CTXPM project",
		"  add            Add and install an external AI resource from a URL",
		"  detect         Detect unmanaged AI resources that may need migration",
		"  migrate        Migrate detected AI resources into ctxpm-managed roots",
		"  install        Install dependencies and repair compatibility links",
		"  list           List dependencies and packages",
		"  validate       Validate ctxpm.yaml and local paths",
		"  check-updates  Check whether dependencies have upstream updates",
		"  update         Apply dependency updates and rewrite manifest versions",
		"  remove         Remove a dependency or package",
		"",
		"Examples:",
		"  ctxpm --version",
		"  ctxpm init --agent codex",
		"  ctxpm add https://github.com/example/ai/tree/main/skills/reviewer --type skill",
		"  ctxpm add https://gitlab.company.com/team/ai-resources.git --type rule --source-path rules/security",
		"  ctxpm install",
		"  ctxpm update --all",
	}
	fmt.Fprintln(w, strings.Join(lines, "\n"))
}

func runVersion() error {
	fmt.Println(cliVersion())
	return nil
}

func cliVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	version := strings.TrimSpace(info.Main.Version)
	if version == "" {
		version = "devel"
	}
	revision := buildInfoSetting(info.Settings, "vcs.revision")
	if revision == "" {
		return version
	}
	short := revision
	if len(short) > 12 {
		short = short[:12]
	}
	if buildInfoSetting(info.Settings, "vcs.modified") == "true" {
		return fmt.Sprintf("%s+%s-dirty", version, short)
	}
	return fmt.Sprintf("%s+%s", version, short)
}

func buildInfoSetting(settings []debug.BuildSetting, key string) string {
	for _, setting := range settings {
		if setting.Key == key {
			return setting.Value
		}
	}
	return ""
}

func reorderArgs(args []string, valueFlags map[string]bool) []string {
	flags := []string{}
	positionals := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if strings.Contains(arg, "=") {
				continue
			}
			if valueFlags[arg] && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	return append(flags, positionals...)
}

func repoRootName(path string) string {
	return filepath.Base(path)
}
