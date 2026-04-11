package cli

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/store"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// NewFrameworkCmd returns the framework parent command with subcommands.
func NewFrameworkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "framework",
		Short: "Manage framework definitions",
	}
	cmd.AddCommand(newFrameworkListCmd())
	cmd.AddCommand(newFrameworkAddCmd())
	cmd.AddCommand(newFrameworkRemoveCmd())
	cmd.AddCommand(newFrameworkSearchCmd())
	cmd.AddCommand(newFrameworkInstallCmd())
	cmd.AddCommand(newFrameworkUpdateCmd())
	return cmd
}

func newFrameworkListCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all available framework definitions",
		Long: `List all framework definitions (built-in, user-defined, and store-installed).

Use --check to compare local definitions against the store and show update status.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runFrameworkList(check)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Compare against the store and show update status")
	return cmd
}

func runFrameworkList(check bool) error {
	frameworks := config.ListFrameworksDetailed()

	// Try to resolve versions from composer.lock in cwd for frameworks
	// that don't have a static version (e.g. built-in laravel).
	cwd, _ := os.Getwd()

	// Fetch store index if --check is requested.
	var storeIndex *store.Index
	if check {
		client := store.NewClient()
		idx, err := client.FetchIndex()
		if err != nil {
			fmt.Printf("[WARN] could not fetch store index: %v\n", err)
		} else {
			storeIndex = idx
		}
	}

	if check {
		fmt.Printf("%-15s %-8s %-10s %-10s %s\n", "Name", "Version", "Source", "Latest", "Status")
		fmt.Printf("%-15s %-8s %-10s %-10s %s\n",
			"───────────────", "────────", "──────────", "──────────", "──────────────────────")
	} else {
		fmt.Printf("%-15s %-8s %-10s %-10s %s\n", "Name", "Version", "Source", "PublicDir", "Workers")
		fmt.Printf("%-15s %-8s %-10s %-10s %s\n",
			"───────────────", "────────", "──────────", "──────────", "──────────────────────")
	}

	for _, info := range frameworks {
		version := info.Version
		if version == "" && cwd != "" {
			version = config.DetectMajorVersion(cwd, info.Name)
		}
		if version == "" {
			version = "—"
		}

		if check {
			latest, status := storeStatus(info, storeIndex)
			fmt.Printf("%-15s %-8s %-10s %-10s %s\n",
				info.Name, version, info.Source, latest, status)
		} else {
			var workerNames []string
			for name := range info.Workers {
				workerNames = append(workerNames, name)
			}
			sort.Strings(workerNames)
			workers := strings.Join(workerNames, ", ")
			if workers == "" {
				workers = "—"
			}
			fmt.Printf("%-15s %-8s %-10s %-10s %s\n",
				info.Name, version, info.Source, info.PublicDir, workers)
		}
	}
	return nil
}

func storeStatus(info config.FrameworkInfo, idx *store.Index) (latest, status string) {
	if idx == nil {
		return "—", "offline"
	}

	for _, entry := range idx.Frameworks {
		if entry.Name != info.Name {
			continue
		}
		latest = entry.Latest

		localVer := info.Version
		if localVer == "" {
			localVer = "0"
		}

		if info.Source == config.SourceBuiltIn {
			return latest, "built-in"
		}

		// Check if a newer version exists in the store.
		found := false
		for _, v := range entry.Versions {
			if v == localVer {
				found = true
				break
			}
		}
		if !found {
			return latest, "not in store"
		}
		if localVer == latest {
			return latest, "up to date"
		}
		return latest, "update available"
	}

	return "—", "not in store"
}

func newFrameworkAddCmd() *cobra.Command {
	var (
		fromFile       string
		label          string
		publicDir      string
		detectFiles    []string
		detectComposer []string
		envFile        string
		envExample     string
		envFormat      string
		composer       string
		npm            string
		create         string
		setupCmds      []string
	)

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add or update a framework definition",
		Long: `Add or update a user-defined framework definition.

Provide a YAML file with --from-file, or specify fields via flags:

  lerd framework add myfw --label "My Framework" --public-dir public \
    --detect-file myfw.php --detect-composer myfw/core

YAML file format:
  name: myfw
  label: My Framework
  public_dir: public
  detect:
    - file: myfw.php
    - composer: myfw/core
  env:
    file: .env
    example_file: .env.example
    format: dotenv
  composer: auto
  npm: auto
  create: composer create-project myvendor/myfw
  setup:
    - label: "Run migrations"
      command: "php bin/console doctrine:migrations:migrate --no-interaction"
      default: true
    - label: "Load fixtures"
      command: "php bin/console doctrine:fixtures:load --no-interaction"
      check:
        composer: doctrine/doctrine-fixtures-bundle`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var fw config.Framework

			if fromFile != "" {
				data, err := os.ReadFile(fromFile)
				if err != nil {
					return fmt.Errorf("reading file: %w", err)
				}
				if err := yaml.Unmarshal(data, &fw); err != nil {
					return fmt.Errorf("parsing YAML: %w", err)
				}
				if fw.Name == "" {
					return fmt.Errorf("framework YAML must include a 'name' field")
				}
			} else {
				if len(args) == 0 {
					return fmt.Errorf("name argument required (or use --from-file)")
				}
				fw.Name = args[0]
				fw.Label = label
				fw.PublicDir = publicDir
				fw.Composer = composer
				fw.NPM = npm
				fw.Create = create

				for _, f := range detectFiles {
					fw.Detect = append(fw.Detect, config.FrameworkRule{File: f})
				}
				for _, c := range detectComposer {
					fw.Detect = append(fw.Detect, config.FrameworkRule{Composer: c})
				}

				if envFile != "" || envExample != "" || envFormat != "" {
					fw.Env = config.FrameworkEnvConf{
						File:        envFile,
						ExampleFile: envExample,
						Format:      envFormat,
					}
				}

				for _, raw := range setupCmds {
					cmdLabel, command, found := strings.Cut(raw, ":")
					if !found {
						return fmt.Errorf("invalid --setup %q: expected \"label:command\"", raw)
					}
					fw.Setup = append(fw.Setup, config.FrameworkSetupCmd{
						Label:   strings.TrimSpace(cmdLabel),
						Command: strings.TrimSpace(command),
						Default: true,
					})
				}
			}

			if fw.PublicDir == "" && fw.Name != "laravel" {
				return fmt.Errorf("--public-dir is required")
			}

			if err := config.SaveFramework(&fw); err != nil {
				return fmt.Errorf("saving framework: %w", err)
			}

			fmt.Printf("Framework %q saved (%s).\n", fw.Name, config.FrameworksDir()+"/"+fw.Name+".yaml")
			if fw.Name == "laravel" {
				fmt.Println("Custom workers merged with built-in Laravel definition.")
			} else {
				fmt.Println("Use 'lerd link' in a project directory to register a site using this framework.")
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&fromFile, "from-file", "f", "", "Load framework definition from a YAML file")
	cmd.Flags().StringVar(&label, "label", "", "Display label (e.g. \"Symfony\")")
	cmd.Flags().StringVar(&publicDir, "public-dir", "", "Document root subdirectory (e.g. public, web)")
	cmd.Flags().StringArrayVar(&detectFiles, "detect-file", nil, "File whose presence signals this framework (repeatable)")
	cmd.Flags().StringArrayVar(&detectComposer, "detect-composer", nil, "Composer package that signals this framework (repeatable)")
	cmd.Flags().StringVar(&envFile, "env-file", "", "Primary env file (default: .env)")
	cmd.Flags().StringVar(&envExample, "env-example", "", "Example env file to copy from when primary is missing")
	cmd.Flags().StringVar(&envFormat, "env-format", "", "Env file format: dotenv or php-const")
	cmd.Flags().StringVar(&composer, "composer", "", "Run composer install: auto, true, or false")
	cmd.Flags().StringVar(&npm, "npm", "", "Run npm install: auto, true, or false")
	cmd.Flags().StringVar(&create, "create", "", "Scaffold command for 'lerd new' (target dir is appended automatically, e.g. \"composer create-project myvendor/myfw\")")
	cmd.Flags().StringArrayVar(&setupCmds, "setup", nil, `Setup command as "label:command" (repeatable, e.g. --setup "Run migrations:php bin/console doctrine:migrations:migrate")`)

	return cmd
}

func newFrameworkRemoveCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "remove <name>[@version]",
		Short: "Remove a framework definition (user-defined or store-installed)",
		Long: `Remove a framework definition. If multiple versions are installed and no
version is specified, you will be prompted to choose which to remove.

Use --all to remove all versions without prompting.

Examples:
  lerd framework remove symfony          # prompt if multiple versions
  lerd framework remove symfony@7        # remove specific version
  lerd framework remove symfony --all    # remove all versions`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name, version := parseNameVersion(args[0])

			if name == "laravel" {
				if err := config.RemoveFramework(name); err != nil {
					if os.IsNotExist(err) {
						return fmt.Errorf("no custom workers defined for laravel")
					}
					return err
				}
				fmt.Printf("Custom laravel overlay removed (built-in definition remains).\n")
				return nil
			}

			files := config.ListFrameworkFiles(name)
			if len(files) == 0 {
				return fmt.Errorf("framework %q not found", name)
			}

			// Specific version requested.
			if version != "" {
				for _, f := range files {
					if f.Version == version {
						if err := config.RemoveFrameworkFile(f.Path); err != nil {
							return err
						}
						fmt.Printf("Removed %s@%s.\n", name, version)
						return nil
					}
				}
				return fmt.Errorf("framework %q version %q not found", name, version)
			}

			// Single file or --all: remove everything.
			if len(files) == 1 || all {
				if err := config.RemoveFramework(name); err != nil {
					return err
				}
				fmt.Printf("Framework %q removed.\n", name)
				return nil
			}

			// Multiple files: prompt.
			fmt.Printf("Multiple definitions found for %q:\n", name)
			labels := make([]string, len(files))
			for i, f := range files {
				v := f.Version
				if v == "" {
					v = "unversioned"
				}
				labels[i] = fmt.Sprintf("%s (%s)", v, f.Source)
				fmt.Printf("  %d) %s\n", i+1, labels[i])
			}
			fmt.Printf("  %d) all\n", len(files)+1)
			fmt.Printf("Choose [1-%d]: ", len(files)+1)

			reader := bufio.NewReader(os.Stdin)
			line, _ := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			choice := 0
			fmt.Sscanf(line, "%d", &choice)

			if choice == len(files)+1 {
				if err := config.RemoveFramework(name); err != nil {
					return err
				}
				fmt.Printf("Framework %q removed (all versions).\n", name)
				return nil
			}

			if choice < 1 || choice > len(files) {
				return fmt.Errorf("invalid choice")
			}

			f := files[choice-1]
			if err := config.RemoveFrameworkFile(f.Path); err != nil {
				return err
			}
			v := f.Version
			if v == "" {
				v = "unversioned"
			}
			fmt.Printf("Removed %s (%s).\n", name, v)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Remove all versions without prompting")
	return cmd
}

func newFrameworkSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search [query]",
		Short: "Search the framework store for available definitions",
		Long: `Search the community framework store. Without a query, lists all available frameworks.

Examples:
  lerd framework search           # list all available
  lerd framework search symfony   # search by name`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			client := store.NewClient()
			results, err := client.Search(query)
			if err != nil {
				return fmt.Errorf("searching store: %w", err)
			}
			if len(results) == 0 {
				fmt.Println("No frameworks found.")
				return nil
			}

			fmt.Printf("%-15s %-15s %-12s %s\n", "Name", "Label", "Latest", "Versions")
			fmt.Printf("%-15s %-15s %-12s %s\n",
				"───────────────", "───────────────", "────────────", "──────────────────────")
			for _, entry := range results {
				fmt.Printf("%-15s %-15s %-12s %s\n",
					entry.Name, entry.Label, entry.Latest, strings.Join(entry.Versions, ", "))
			}
			return nil
		},
	}
}

func newFrameworkInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <name>[@version]",
		Short: "Install a framework definition from the store",
		Long: `Download and install a framework definition from the community store.

If no version is specified, the version is auto-detected from composer.lock
in the current directory, falling back to the latest available version.

Examples:
  lerd framework install symfony
  lerd framework install laravel@11
  lerd framework install wordpress@6`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name, version := parseNameVersion(args[0])

			client := store.NewClient()

			// Auto-detect version from cwd if not specified
			if version == "" {
				cwd, _ := os.Getwd()
				if cwd != "" {
					if idx, err := client.FetchIndex(); err == nil {
						for _, entry := range idx.Frameworks {
							if entry.Name == name {
								version = store.ResolveVersion(cwd, entry.Detect, entry.Versions, "")
								break
							}
						}
					}
				}
			}

			fw, err := client.FetchFramework(name, version)
			if err != nil {
				return err
			}

			// Check if already exists locally
			if _, ok := config.GetFramework(name); ok {
				fmt.Printf("Framework %q already exists locally. Overwriting with store definition.\n", name)
			}

			if err := config.SaveStoreFramework(fw); err != nil {
				return fmt.Errorf("saving framework: %w", err)
			}
			// Remove old user-defined file so the store version takes effect.
			config.RemoveUserFramework(name)

			versionStr := fw.Version
			if versionStr == "" {
				versionStr = "latest"
			}
			filename := fw.Name + ".yaml"
			if fw.Version != "" {
				filename = fw.Name + "@" + fw.Version + ".yaml"
			}
			fmt.Printf("Installed %s@%s (%s).\n", fw.Name, versionStr, fw.Label)
			fmt.Printf("Saved to %s/%s\n", config.StoreFrameworksDir(), filename)
			return nil
		},
	}
}

func newFrameworkUpdateCmd() *cobra.Command {
	var diff bool
	cmd := &cobra.Command{
		Use:   "update [name[@version]]",
		Short: "Update installed framework definitions from the store",
		Long: `Re-fetch framework definitions from the store.

If a name is given, only that framework is updated.
If no name is given, all locally installed store frameworks are updated.
Use --diff to preview changes before applying.

Examples:
  lerd framework update symfony       # update to latest
  lerd framework update symfony@7     # update specific version
  lerd framework update               # update all
  lerd framework update --diff        # show what would change`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			client := store.NewClient()

			if len(args) == 1 {
				name, version := parseNameVersion(args[0])
				return updateSingleFramework(client, name, version, diff)
			}
			return updateAllFrameworks(client, diff)
		},
	}
	cmd.Flags().BoolVar(&diff, "diff", false, "Show changes before applying")
	return cmd
}

func updateSingleFramework(client *store.Client, name, version string, showDiff bool) error {
	if version == "" {
		cwd, _ := os.Getwd()
		version = autoDetectVersion(client, name, cwd)
	}
	remote, err := client.FetchFramework(name, version)
	if err != nil {
		return err
	}

	local, _ := config.GetFramework(name)
	if showDiff && local != nil {
		changed, err := showFrameworkDiff(name, local, remote)
		if err != nil {
			return err
		}
		if !changed {
			fmt.Printf("%s@%s is already up to date.\n", name, versionOrLatest(remote))
			return nil
		}
	}

	if err := config.SaveStoreFramework(remote); err != nil {
		return fmt.Errorf("saving framework: %w", err)
	}
	// Remove old user-defined file if it exists — the store version should take effect.
	config.RemoveUserFramework(name)
	fmt.Printf("Updated %s@%s (%s).\n", remote.Name, versionOrLatest(remote), remote.Label)
	return nil
}

func updateAllFrameworks(client *store.Client, showDiff bool) error {
	idx, err := client.FetchIndex()
	if err != nil {
		return err
	}

	local := config.ListFrameworksDetailed()
	updated := 0
	for _, info := range local {
		if info.Source == config.SourceBuiltIn {
			continue
		}
		for _, entry := range idx.Frameworks {
			if entry.Name != info.Name {
				continue
			}
			remote, fetchErr := client.FetchFramework(entry.Name, entry.Latest)
			if fetchErr != nil {
				fmt.Printf("  [WARN] %s: %v\n", entry.Name, fetchErr)
				continue
			}

			if showDiff {
				changed, diffErr := showFrameworkDiff(entry.Name, info.Framework, remote)
				if diffErr != nil {
					fmt.Printf("  [WARN] %s: %v\n", entry.Name, diffErr)
					continue
				}
				if !changed {
					fmt.Printf("  %s@%s — up to date\n", entry.Name, versionOrLatest(remote))
					continue
				}
			}

			if saveErr := config.SaveStoreFramework(remote); saveErr != nil {
				fmt.Printf("  [WARN] %s: %v\n", entry.Name, saveErr)
				continue
			}
			config.RemoveUserFramework(entry.Name)
			fmt.Printf("  Updated %s@%s\n", remote.Name, versionOrLatest(remote))
			updated++
			break
		}
	}
	if updated == 0 {
		fmt.Println("No frameworks to update.")
	} else {
		fmt.Printf("Updated %d framework(s).\n", updated)
	}
	return nil
}

func versionOrLatest(fw *config.Framework) string {
	if fw.Version != "" {
		return fw.Version
	}
	return "latest"
}

// showFrameworkDiff prints a colored diff between local and remote framework
// definitions. Returns true if they differ.
func showFrameworkDiff(name string, local, remote *config.Framework) (bool, error) {
	localYAML, err := yaml.Marshal(local)
	if err != nil {
		return false, err
	}
	remoteYAML, err := yaml.Marshal(remote)
	if err != nil {
		return false, err
	}
	if string(localYAML) == string(remoteYAML) {
		return false, nil
	}

	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(localYAML)),
		B:        difflib.SplitLines(string(remoteYAML)),
		FromFile: name + " (local)",
		ToFile:   name + " (store)",
		Context:  3,
	})
	if err != nil {
		return true, err
	}

	fmt.Printf("\n%s:\n", name)
	for _, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "+"):
			fmt.Printf("\033[32m%s\033[0m\n", line)
		case strings.HasPrefix(line, "-"):
			fmt.Printf("\033[31m%s\033[0m\n", line)
		case strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			fmt.Printf("\033[34m%s\033[0m\n", line)
		default:
			fmt.Println(line)
		}
	}
	fmt.Println()
	return true, nil
}

func autoDetectVersion(client *store.Client, name, dir string) string {
	if dir == "" {
		return ""
	}
	idx, err := client.FetchIndex()
	if err != nil {
		return ""
	}
	for _, entry := range idx.Frameworks {
		if entry.Name == name {
			return store.ResolveVersion(dir, entry.Detect, entry.Versions, "")
		}
	}
	return ""
}

// parseNameVersion splits "name@version" into (name, version).
func parseNameVersion(s string) (string, string) {
	if i := strings.IndexByte(s, '@'); i != -1 {
		return s[:i], s[i+1:]
	}
	return s, ""
}
