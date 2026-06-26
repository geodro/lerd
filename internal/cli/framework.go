package cli

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/feedback"
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
	cmd.AddCommand(newFrameworkPruneCmd())
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
			feedback.Warn("could not fetch store index: %v", err)
		} else {
			storeIndex = idx
		}
	}

	var headers []string
	if check {
		headers = []string{"Name", "Version", "Source", "Latest", "Status"}
	} else {
		headers = []string{"Name", "Version", "Source", "PublicDir", "Workers"}
	}

	rows := make([][]string, 0, len(frameworks))
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
			rows = append(rows, []string{info.Name, version, string(info.Source), latest, status})
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
			rows = append(rows, []string{info.Name, version, string(info.Source), info.PublicDir, workers})
		}
	}
	feedback.Table(headers, rows)
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

			feedback.Begin()
			feedback.Done(fmt.Sprintf("framework %q saved (%s)", fw.Name, config.FrameworksDir()+"/"+fw.Name+".yaml"))
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
	var force bool
	cmd := &cobra.Command{
		Use:   "remove <name>[@version]",
		Short: "Remove a framework definition (user-defined or store-installed)",
		Long: `Remove a framework definition. If multiple versions are installed and no
version is specified, you will be prompted to choose which to remove.

If any linked site still uses the framework, you are asked to confirm first.

Use --all to remove all versions without prompting, and --force to skip the
in-use confirmation.

Examples:
  lerd framework remove symfony          # prompt if multiple versions
  lerd framework remove symfony@7        # remove specific version
  lerd framework remove symfony --all    # remove all versions
  lerd framework remove symfony --force  # skip the in-use confirmation`,
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
				feedback.Done("custom laravel overlay removed (built-in definition remains)")
				return nil
			}

			files := config.ListFrameworkFiles(name)
			if len(files) == 0 {
				return fmt.Errorf("framework %q not found", name)
			}

			// Specific version requested: this removes one cached definition, not
			// the framework, so the name-level in-use guard does not apply.
			if version != "" {
				for _, f := range files {
					if f.Version == version {
						if err := config.RemoveFrameworkFile(f.Path); err != nil {
							return err
						}
						feedback.Done(fmt.Sprintf("removed %s@%s", name, version))
						return nil
					}
				}
				return fmt.Errorf("framework %q version %q not found", name, version)
			}

			// Removing every definition for the name. Confirm if an active site
			// still uses it, unless it is built-in (the binary definition remains,
			// so a site never breaks) or --force is given.
			if !force && !config.IsBuiltinFramework(name) {
				if sites := config.SitesUsingFramework(name); len(sites) > 0 {
					fmt.Printf("Framework %q is still used by %s: %s\n",
						name, pluralizeSites(len(sites)), strings.Join(sites, ", "))
					if !promptConfirm("Remove it anyway?") {
						feedback.Note("aborted")
						return nil
					}
				}
			}

			builtinNote := ""
			if config.IsBuiltinFramework(name) {
				builtinNote = " (built-in definition remains)"
			}

			// Single file or --all: remove everything.
			if len(files) == 1 || all {
				if err := config.RemoveFramework(name); err != nil {
					return err
				}
				feedback.Done(fmt.Sprintf("framework %q removed%s", name, builtinNote))
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
				feedback.Done(fmt.Sprintf("framework %q removed (all versions)%s", name, builtinNote))
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
			feedback.Done(fmt.Sprintf("removed %s (%s)", name, v))
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Remove all versions without prompting")
	cmd.Flags().BoolVar(&force, "force", false, "Skip the confirmation when a site still uses the framework")
	return cmd
}

func pluralizeSites(n int) string {
	if n == 1 {
		return "1 site"
	}
	return fmt.Sprintf("%d sites", n)
}

func newFrameworkPruneCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove installed framework definitions no site uses",
		Long: `Remove store-installed and user-defined framework definitions that no linked
site references. Built-in definitions are never touched.

This is safe: lerd re-fetches a definition from the store automatically the
moment a site needs one that is no longer present locally, so a pruned
framework comes back on its own if it is needed again.

Examples:
  lerd framework prune          # list unused, then confirm
  lerd framework prune --force  # remove without confirming`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			unused := config.UnusedInstalledFrameworks()
			if len(unused) == 0 {
				feedback.Done("no unused frameworks to prune")
				return nil
			}

			fmt.Printf("The following %s used by any site:\n", pruneSubject(len(unused)))
			for _, name := range unused {
				fmt.Printf("  %s\n", name)
			}

			if !force && !promptConfirm("Remove them?") {
				feedback.Note("aborted")
				return nil
			}

			removed, failed := config.RemoveFrameworks(unused)
			for _, name := range failed {
				feedback.Warn("could not remove %q", name)
			}
			if len(removed) == 0 {
				return fmt.Errorf("could not prune any frameworks")
			}
			feedback.Done(fmt.Sprintf("pruned %s", pluralizeFrameworks(len(removed))))
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Remove without confirming")
	return cmd
}

func pruneSubject(n int) string {
	if n == 1 {
		return "framework is not"
	}
	return "frameworks are not"
}

func pluralizeFrameworks(n int) string {
	if n == 1 {
		return "1 framework"
	}
	return fmt.Sprintf("%d frameworks", n)
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

			rows := make([][]string, 0, len(results))
			for _, entry := range results {
				rows = append(rows, []string{
					entry.Name, entry.Label, entry.Latest, strings.Join(entry.Versions, ", "),
				})
			}
			feedback.Table([]string{"Name", "Label", "Latest", "Versions"}, rows)
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
			feedback.Done(fmt.Sprintf("installed %s@%s (%s)", fw.Name, versionStr, fw.Label))
			feedback.Note("saved to " + config.StoreFrameworksDir() + "/" + filename)
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
If no name is given, every cached version of every installed framework is
re-fetched (e.g. laravel@10, @11, @12, @13 are all refreshed individually,
not just the latest).
Use --diff to preview changes before applying.

Examples:
  lerd framework update symfony       # update to latest
  lerd framework update symfony@7     # update specific version
  lerd framework update               # update all (every cached version)
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
	feedback.Done(fmt.Sprintf("updated %s@%s (%s)", remote.Name, versionOrLatest(remote), remote.Label))
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
		// Refresh the same version that's cached; previously this fetched
		// entry.Latest for every entry, so users with multiple versions
		// (laravel@10..@13) ended up only refreshing the latest file and
		// the others stayed stale forever.
		inIndex := false
		for _, entry := range idx.Frameworks {
			if entry.Name == info.Name {
				inIndex = true
				break
			}
		}
		if !inIndex {
			continue
		}
		remote, fetchErr := client.FetchFramework(info.Name, info.Version)
		if fetchErr != nil {
			feedback.Warn("%s@%s: %v", info.Name, info.Version, fetchErr)
			continue
		}

		if showDiff {
			changed, diffErr := showFrameworkDiff(info.Name, info.Framework, remote)
			if diffErr != nil {
				feedback.Warn("%s@%s: %v", info.Name, info.Version, diffErr)
				continue
			}
			if !changed {
				fmt.Printf("  %s@%s — up to date\n", info.Name, versionOrLatest(remote))
				continue
			}
		}

		if saveErr := config.SaveStoreFramework(remote); saveErr != nil {
			feedback.Warn("%s@%s: %v", info.Name, info.Version, saveErr)
			continue
		}
		config.RemoveUserFramework(info.Name)
		feedback.Note(fmt.Sprintf("updated %s@%s", remote.Name, versionOrLatest(remote)))
		updated++
	}
	if updated == 0 {
		feedback.Line("no frameworks to update")
	} else {
		feedback.Done(fmt.Sprintf("updated %d framework(s)", updated))
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
		case strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			fmt.Println(feedback.Dim(line))
		case strings.HasPrefix(line, "+"):
			fmt.Println(feedback.Green(line))
		case strings.HasPrefix(line, "-"):
			fmt.Println(feedback.Red(line))
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
