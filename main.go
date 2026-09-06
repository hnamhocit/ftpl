package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// verbose bật output [debug] cho mọi command.
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
			fmt.Printf("[debug] config: dev_url=%s schema=%s dir=%s\n",
				migrateCfg.DevURL, migrateCfg.Schema, migrateCfg.Dir)
		}
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
	Aliases: []string{"rm", "delete"}, // FIX (4): bỏ "delete resource" có khoảng trắng
	Short:   "Delete a resource",
	Args:    cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		deleteResource(args[0])
	},
}

var newCmd = &cobra.Command{
	Use:   "new <app>",
	Short: "Create a new project (coming soon)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("ftpl new: coming soon")
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug output")

	// FIX (3): chuyển 3 flag override lên rootCmd để dbCmd cũng dùng được.
	// Trước đây nằm ở migrateCmd.PersistentFlags() → dbCmd là "anh em" của migrateCmd, không thừa hưởng.
	rootCmd.PersistentFlags().String("dev-url", "", "override dev database URL")
	rootCmd.PersistentFlags().String("schema", "", "override schema file path")
	rootCmd.PersistentFlags().String("dir", "", "override migrations directory")

	rootCmd.AddCommand(generateCmd, deleteResourceCmd, newCmd, migrateCmd, dbCmd, doctorCmd)
	generateCmd.AddCommand(resourceCmd)

	resourceCmd.Flags().Bool("no-tests", false, "skip test files")
	resourceCmd.Flags().Bool("no-crud", false, "generate wiring skeleton only")
	resourceCmd.Flags().BoolP("yes", "y", false, "accept defaults, skip prompts")
	resourceCmd.Flags().String("type", "restful", "transport: restful (graphql/websocket/socket coming soon)")

	resourceCmd.SetFlagErrorFunc(flagErrorFunc)
}
