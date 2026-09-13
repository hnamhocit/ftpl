package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// verbose enables [debug] output for every command.
var verbose bool

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:          "ftpl",
	Short:        "Fiber template CLI",
	SilenceUsage: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		migrateCfg = loadMigrateConfig()
		if verbose {
			fmt.Printf("[debug] config: dev_url=shadow db schema=%s dir=%s\n",
				migrateCfg.Schema, migrateCfg.Dir)
		}
		checkUpdateOncePerSession(cmd)
	},
}

var generateCmd = &cobra.Command{
	Use:     "generate",
	Aliases: []string{"g"},
	Short:   "Generate code",
}

var resourceCmd = &cobra.Command{
	Use:     "resource <name>",
	Aliases: []string{"res", "feature"},
	Short:   "Generate a feature (handler, service, repository, dto, tests)",
	Args:    cobra.ExactArgs(1),
	Run:     runGenerate,
}

var deleteResourceCmd = &cobra.Command{
	Use:     "dr <name>",
	Aliases: []string{"rm", "delete"},
	Short:   "Delete a resource",
	Args:    cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		deleteResource(args[0])
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug output")

	// Flag overrides live on rootCmd so migrateCmd AND dbCmd both inherit them.
	rootCmd.PersistentFlags().String("dev-url", "", "override dev database URL (skip shadow db)")
	rootCmd.PersistentFlags().String("schema", "", "override schema file path")
	rootCmd.PersistentFlags().String("dir", "", "override migrations directory")

	rootCmd.AddCommand(generateCmd, deleteResourceCmd, newCmd, migrateCmd, dbCmd, doctorCmd, runCmd, versionCmd, updateCmd, uninstallCmd, newCmd)
	generateCmd.AddCommand(resourceCmd)

	resourceCmd.Flags().Bool("no-tests", false, "skip test files")
	resourceCmd.Flags().Bool("no-crud", false, "generate wiring skeleton only")
	resourceCmd.Flags().BoolP("yes", "y", false, "accept defaults, skip prompts")
	resourceCmd.Flags().String("type", "restful", "transport: restful (graphql/websocket/socket coming soon)")

	resourceCmd.SetFlagErrorFunc(flagErrorFunc)
}
