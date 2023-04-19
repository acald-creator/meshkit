package coder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/layer5io/meshkit/cmd/errorutil/internal/component"

	"github.com/layer5io/meshkit/cmd/errorutil/internal/config"
	errutilerr "github.com/layer5io/meshkit/cmd/errorutil/internal/error"
	"github.com/spf13/cobra"
)

const (
	verboseCmdFlag             = "verbose"
	rootDirCmdFlag             = "dir"
	skipDirsCmdFlag            = "skip-dirs"
	outDirCmdFlag              = "out-dir"
	infoDirCmdFlag             = "info-dir"
	forceUpdateAllCodesCmdFlag = "force"
)

type globalFlags struct {
	verbose                  bool
	rootDir, outDir, infoDir string
	skipDirs                 []string
}

func defaultIfEmpty(value, defaultValue string) string {
	if len(value) == 0 {
		return defaultValue
	}
	return value
}

func getGlobalFlags(cmd *cobra.Command) (globalFlags, error) {
	flags := globalFlags{}
	flagMap := map[string]interface{}{
		verboseCmdFlag:  &flags.verbose,
		rootDirCmdFlag:  &flags.rootDir,
		skipDirsCmdFlag: &flags.skipDirs,
		outDirCmdFlag:   &flags.outDir,
		infoDirCmdFlag:  &flags.infoDir,
	}

	for flagName, flagPtr := range flagMap {
		flag := cmd.Flags().Lookup(flagName)
		if flag == nil {
			return flags, fmt.Errorf("flag not found: %s", flagName)
		}

		switch ptr := flagPtr.(type) {
		case *bool:
			value, err := cmd.Flags().GetBool(flagName)
			if err != nil {
				return flags, err
			}
			*ptr = value
		case *string:
			value, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return flags, err
			}
			if flagName == outDirCmdFlag || flagName == infoDirCmdFlag {
				*ptr = defaultIfEmpty(value, flags.rootDir)
			} else {
				*ptr = value
			}
		case *[]string:
			value, err := cmd.Flags().GetStringSlice(flagName)
			if err != nil {
				return flags, err
			}
			*ptr = value
		default:
			return flags, fmt.Errorf("unsupported flag type for flag: %s", flagName)
		}
	}

	return flags, nil
}

func walkAndUpdateErrorsInfo(globalFlags globalFlags, update bool, updateAll bool, errorsInfo *errutilerr.InfoAll) error {
	config.Logger(globalFlags.verbose)
	err := walk(globalFlags, update, updateAll, errorsInfo)
	if err != nil {
		return err
	}
	return nil
}

func walkSummarizeExport(globalFlags globalFlags, update bool, updateAll bool) error {
	errorsInfo := errutilerr.NewInfoAll()

	err := walkAndUpdateErrorsInfo(globalFlags, update, updateAll, errorsInfo)
	if err != nil {
		return err
	}

	if update {
		errorsInfo = errutilerr.NewInfoAll()
		err = walkAndUpdateErrorsInfo(globalFlags, false, false, errorsInfo)
		if err != nil {
			return err
		}
	}

	jsn, err := json.MarshalIndent(errorsInfo, "", "  ")
	if err != nil {
		return err
	}
	fname := filepath.Join(globalFlags.outDir, config.App+"_analyze_errors.json")
	err = os.WriteFile(fname, jsn, 0600)
	if err != nil {
		return err
	}

	componentInfo, err := component.New(globalFlags.infoDir)
	if err != nil {
		return err
	}

	err = errutilerr.SummarizeAnalysis(componentInfo, errorsInfo, globalFlags.outDir)
	if err != nil {
		return err
	}

	return errutilerr.Export(componentInfo, errorsInfo, globalFlags.outDir)
}

func commandAnalyze() *cobra.Command {
	return &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a directory tree",
		Long:  `analyze analyzes a directory tree for error codes`,
		Args:  cobra.MinimumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			globalFlags, err := getGlobalFlags(cmd)
			if err != nil {
				return err
			}
			return walkSummarizeExport(globalFlags, false, false)
		},
	}
}

func commandUpdate() *cobra.Command {
	var updateAll bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update error codes and details",
		Long:  "update replaces error codes where specified, and updates error details",
		Args:  cobra.MinimumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			globalFlags, err := getGlobalFlags(cmd)
			if err != nil {
				return err
			}
			updateAll, err := cmd.Flags().GetBool(forceUpdateAllCodesCmdFlag)
			if err != nil {
				return err
			}
			return walkSummarizeExport(globalFlags, true, updateAll)
		},
	}
	cmd.PersistentFlags().BoolVar(&updateAll, forceUpdateAllCodesCmdFlag, false, "Update and re-sequence all error codes.")
	return cmd
}

func commandDoc() *cobra.Command {
	return &cobra.Command{
		Use:   "doc",
		Short: "Print the documentation",
		Long:  "Print the documentation",
		Run: func(cmd *cobra.Command, args []string) {
			println(`
This tool analyzes, verifies and updates MeshKit compatible errors in Meshery Go source code trees.

A MeshKit compatible error consist of
- An error code defined as a constant or variable (preferably constant), of type string.
  - The naming convention for these variables is the regex "^Err[A-Z].+Code$", e.g. ErrApplyManifestCode.
  - The initial value of the code is a placeholder string, e.g. "replace_me", set by the developer.
  - The final value of the code is an integer, set by this tool, as part of a CI workflow.
- Error details defined using the function errors.New(code, severity, sdescription, ldescription, probablecause, remedy) from MeshKit.
 - The first parameter, 'code', has to be passed as the error code constant (or variable), not a string literal.
 - The second parameter, 'severity', has its own type; consult its Go-doc for further details.
 - The remaining parameters are string arrays for short and long description, probable cause, and suggested remediation.
 - Use string literals in these string arrays, not constants or variables, for any static texts.
 - Capitalize the first letter of each statement.
 - Call expressions can be used but will be ignored by the tool when exporting error details for the documentation.
 - Do not concatenate strings using the '+' operator, just add multiple elements to the string array.

Additionally, the following conventions apply:
- Errors are defined in each package, in a file named error.go
- Errors are namespaced to components, i.e. they need to be unique within a component (see below).
- Errors are not to be reused across components and modules.
- There are no predefined error code ranges for components. Every component is free to use its own range.
- Codes carry no meaning, as e.g. HTTP status codes do.

This tool produces three files:
- errorutil_analyze_errors.json: raw data with all errors and some metadata
- errorutil_analyze_summary.json: summary of raw data, also used for validation and troubleshooting
- errorutil_errors_export.json: export of errors which can be used to create the error code reference on the Meshery website

Typically, the 'analyze' command of the tool is used by the developer to verify errors, i.e. that there are no duplicate names or details.
A CI workflow is used to replace the placeholder code strings with integer code, and export errors. Using this export, the workflow updates 
the error code reference documentation in the Meshery repository.

Meshery components and this tool:
- Meshery components have a name and a type.
- An example of a component is MeshKit with 'meshkit' as name, and 'library' as type.
- Often, a specific component corresponds to one git repository.
- The tool requires a file called component_info.json.
  This file has the following content, with concrete values specific for each component:
  {
    "name": "meshkit",
    "type": "library",
    "next_error_code": 1014
  }
- next_error_code is the value used by the tool to replace the error code placeholder string with the next integer.
- The tool updates next_error_code. 
`)
		},
	}
}

type RootFlags struct {
	Verbose  bool
	RootDir  string
	OutDir   string
	InfoDir  string
	SkipDirs []string
}

func setupRootFlags(cmd *cobra.Command, flags *RootFlags) {
	cmd.PersistentFlags().BoolVarP(&flags.Verbose, verboseCmdFlag, "v", false, "verbose output")
	cmd.PersistentFlags().StringVarP(&flags.RootDir, rootDirCmdFlag, "d", ".", "root directory")
	cmd.PersistentFlags().StringVarP(&flags.OutDir, outDirCmdFlag, "o", "", "output directory")
	cmd.PersistentFlags().StringVarP(&flags.InfoDir, infoDirCmdFlag, "i", "", "directory containing the component_info.json file")
	cmd.PersistentFlags().StringSliceVar(&flags.SkipDirs, skipDirsCmdFlag, []string{}, "directories to skip (comma-separated list, repeatable argument)")
}

func RootCommand() *cobra.Command {
	cmd := &cobra.Command{Use: config.App}
	flags := &RootFlags{}
	setupRootFlags(cmd, flags)

	cmd.AddCommand(commandAnalyze())
	cmd.AddCommand(commandUpdate())
	cmd.AddCommand(commandDoc())

	return cmd
}
