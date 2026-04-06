package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/geodro/lerd/internal/config"
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
	return cmd
}

func newFrameworkListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all available framework definitions",
		RunE: func(_ *cobra.Command, _ []string) error {
			frameworks := config.ListFrameworks()
			fmt.Printf("%-15s %-15s %-10s %s\n", "Name", "Label", "PublicDir", "Detect")
			fmt.Printf("%-15s %-15s %-10s %s\n",
				"───────────────", "───────────────", "──────────", "──────────────────────")
			for _, fw := range frameworks {
				var rules []string
				for _, r := range fw.Detect {
					if r.File != "" {
						rules = append(rules, "file:"+r.File)
					}
					if r.Composer != "" {
						rules = append(rules, "composer:"+r.Composer)
					}
				}
				detect := strings.Join(rules, ", ")
				if detect == "" {
					detect = "—"
				}
				fmt.Printf("%-15s %-15s %-10s %s\n", fw.Name, fw.Label, fw.PublicDir, detect)
			}
			return nil
		},
	}
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
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a user-defined framework definition",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if err := config.RemoveFramework(name); err != nil {
				if os.IsNotExist(err) {
					if name == "laravel" {
						return fmt.Errorf("no custom workers defined for laravel")
					}
					return fmt.Errorf("framework %q not found", name)
				}
				return err
			}
			fmt.Printf("Framework %q removed.\n", name)
			return nil
		},
	}
}
