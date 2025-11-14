package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pulumi/pulumi-terraform-migrate/pkg/tfmig"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optimport"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
)

var nextCmd = &cobra.Command{
	Use:   "next",
	Short: "Suggest the next step in the migration process",
	Long: `Analyzes the current state of the migration and suggests the next step to take.

This command examines the migration.json file, Pulumi stacks, and Terraform state to determine
what action should be taken next in the migration process.

Example:
  pulumi-terraform-migrate next --migration migration.json`,

	Run: func(cmd *cobra.Command, args []string) {
		migrationFile := cmd.Flag("migration").Value.String()

		next(migrationFile)
	},
}

func init() {
	rootCmd.AddCommand(nextCmd)
	nextCmd.Flags().String("migration", "migration.json", "Path to migration.json file")
}

func next(migrationFile string) {
	ctx := context.Background()

	// Check that the migration file exists
	if !ensureMigrationFileExists(migrationFile) {
		return
	}

	// Load the migration file
	mf, err := tfmig.LoadMigration(migrationFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading migration file: %v\n", err)
		os.Exit(1)
	}

	// Check Pulumi stacks
	if !ensurePulumiStacksExist(ctx, mf) {
		return
	}

	// Check that Pulumi source code seems to be generated and migration.json has complete resource mappings.
	if !ensureSourceCodeMapped(ctx, mf, migrationFile) {
		return
	}

	// Ensure import-stub.json files exist for all stacks
	if !ensureImportStubsExist(ctx, mf, migrationFile) {
		return
	}

	// Ensure import-stub.json files are resolved to import.json files
	if !ensureImportStubsResolved(ctx, mf, migrationFile) {
		return
	}

	fmt.Println("STOP")
}

func ensureMigrationFileExists(migrationFile string) bool {
	migrationFileExists := false
	if migrationFile != "" {
		if _, err := os.Stat(migrationFile); err == nil {
			migrationFileExists = true
		}
	} else {
		if _, err := os.Stat("migration.json"); err == nil {
			migrationFile = "migration.json"
			migrationFileExists = true
		}
	}

	// Check if migration file exists
	if !migrationFileExists {
		fmt.Printf("Migration file '%s' does not exist.\n\n", migrationFile)
		fmt.Println("To get started, initialize a migration file by running:")
		fmt.Println()
		fmt.Println("  pulumi-terraform-migrate init-migration \\")
		fmt.Println("    --migration migration.json \\")
		fmt.Println("    --tf-sources <path-to-terraform-sources> \\")
		fmt.Println("    --pulumi-sources <path-to-pulumi-project>")
		fmt.Println()
		return false
	}
	return true
}

func ensurePulumiStacksExist(ctx context.Context, mf *tfmig.MigrationFile) bool {
	missingStacks, err := checkPulumiStacks(ctx, mf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking Pulumi stacks: %v\n", err)
		os.Exit(1)
	}

	// If there are missing stacks, suggest creating them
	if len(missingStacks) > 0 {
		fmt.Println("The following Pulumi stacks do not exist:")
		fmt.Println()
		for _, stackName := range missingStacks {
			fmt.Printf("  - %s\n", stackName)
		}
		fmt.Println()
		fmt.Printf("Please create the missing Pulumi stacks by running these commands in %q:",
			mf.Migration.PulumiSources)
		fmt.Println()
		fmt.Println()
		for _, stackName := range missingStacks {
			fmt.Printf("  pulumi stack init %s\n", stackName)
		}
		fmt.Println()
		return false
	}

	return true
}

// checkPulumiStacks checks which stacks in the migration file do not exist
func checkPulumiStacks(ctx context.Context, mf *tfmig.MigrationFile) ([]string, error) {
	// Create a workspace pointing to the Pulumi project
	ws, err := auto.NewLocalWorkspace(ctx, auto.WorkDir(mf.Migration.PulumiSources))
	if err != nil {
		return nil, fmt.Errorf("failed to create Pulumi workspace: %w", err)
	}

	// Get list of existing stacks
	existingStacks, err := ws.ListStacks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list stacks: %w", err)
	}

	// Create a set of existing stack names
	existingStackSet := make(map[string]bool)
	for _, stack := range existingStacks {
		existingStackSet[stack.Name] = true
	}

	// Check which stacks are missing
	var missingStacks []string
	for _, stack := range mf.Migration.Stacks {
		if !existingStackSet[stack.PulumiStack] {
			missingStacks = append(missingStacks, stack.PulumiStack)
		}
	}

	return missingStacks, nil
}

type previewError struct {
	stackName string
	command   string
	err       error
	stdout    string
	stderr    string
}

func ensureImportStubsExist(ctx context.Context, mf *tfmig.MigrationFile, migrationFile string) bool {
	// Create workspace
	ws, err := auto.NewLocalWorkspace(ctx, auto.WorkDir(mf.Migration.PulumiSources))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating Pulumi workspace: %v\n", err)
		os.Exit(1)
	}

	// Track if we need to save the migration file
	needsSave := false
	var previewErrors []previewError

	// Process each stack
	for i := range mf.Migration.Stacks {
		stack := &mf.Migration.Stacks[i]

		// Check if import-stub-file is already set
		if stack.ImportStubFile != "" {
			// Verify the file exists
			if _, err := os.Stat(stack.ImportStubFile); err == nil {
				continue // File exists, skip this stack
			}
			// File doesn't exist, regenerate
		}

		// Generate import-stub.json for this stack in a temporary file
		tmpFile, err := os.CreateTemp("", fmt.Sprintf("import-stub-%s-*.json", stack.PulumiStack))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating temporary file: %v\n", err)
			os.Exit(1)
		}
		importStubPath := tmpFile.Name()
		tmpFile.Close()

		// Select the stack
		s, err := auto.SelectStack(ctx, stack.PulumiStack, ws)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error selecting stack %s: %v\n", stack.PulumiStack, err)
			os.Exit(1)
		}

		// Capture stdout/stderr from preview
		var stdoutBuf, stderrBuf strings.Builder

		// Run preview with import file
		command := fmt.Sprintf("pulumi preview --stack %s --import-file %s", stack.PulumiStack, importStubPath)
		_, err = s.Preview(ctx,
			optpreview.ImportFile(importStubPath),
			optpreview.ProgressStreams(&stdoutBuf),
			optpreview.ErrorProgressStreams(&stderrBuf),
		)
		if err != nil {
			previewErrors = append(previewErrors, previewError{
				stackName: stack.PulumiStack,
				command:   command,
				err:       err,
				stdout:    stdoutBuf.String(),
				stderr:    stderrBuf.String(),
			})
		}

		// Update the import-stub-file path in the migration file
		stack.ImportStubFile = importStubPath
		needsSave = true
	}

	// Save the migration file if we updated any paths
	if needsSave {
		if err := mf.Save(migrationFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving migration file: %v\n", err)
			os.Exit(1)
		}
	}

	// If there were errors, report them
	if len(previewErrors) > 0 {
		fmt.Println("Failed to generate import-stub.json files for one or more stacks:")
		fmt.Println()
		for _, perr := range previewErrors {
			fmt.Println()
			fmt.Printf("Stack %q:\n", perr.stackName)
			fmt.Println()
			fmt.Printf("  Command: %s\n", perr.command)
			fmt.Printf("  Error: %v\n", perr.err)
			if perr.stdout != "" {
				fmt.Println("  Stdout:")
				fmt.Println(indent(perr.stdout, "    "))
			}
			if perr.stderr != "" {
				fmt.Println("  Stderr:")
				fmt.Println(indent(perr.stderr, "    "))
			}
			fmt.Println()
		}
		fmt.Println("Please fix the issues above before proceeding.")
		return false
	}

	return true
}

func indent(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

type resolveError struct {
	stackName string
	command   string
	err       error
}

func ensureImportStubsResolved(ctx context.Context, mf *tfmig.MigrationFile, migrationFile string) bool {
	// Track if we need to save the migration file
	needsSave := false
	var resolveErrors []resolveError

	// Process each stack
	for i := range mf.Migration.Stacks {
		stack := &mf.Migration.Stacks[i]

		// Check if import-resolved-file is already set and exists
		if stack.ImportResolvedFile != "" {
			if _, err := os.Stat(stack.ImportResolvedFile); err == nil {
				continue // File exists, skip this stack
			}
			// File doesn't exist, regenerate
		}

		// Create a temporary file for the resolved import file
		tmpFile, err := os.CreateTemp("", fmt.Sprintf("import-resolved-%s-*.json", stack.PulumiStack))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating temporary file: %v\n", err)
			os.Exit(1)
		}
		outputPath := tmpFile.Name()
		tmpFile.Close()

		// Call the resolve implementation directly
		opts := tfmig.ResolveImportStubsOptions{
			MigrationFile: migrationFile,
			StackName:     stack.PulumiStack,
			StubsFile:     stack.ImportStubFile,
		}

		result, err := tfmig.ResolveImportStubs(opts)
		if err != nil {
			command := fmt.Sprintf("pulumi-terraform-migrate resolve-import-stubs --migration %s --stack %s --stubs %s --out %s",
				migrationFile, stack.PulumiStack, stack.ImportStubFile, outputPath)
			resolveErrors = append(resolveErrors, resolveError{
				stackName: stack.PulumiStack,
				command:   command,
				err:       err,
			})
			continue
		}

		// Write the resolved import file
		outputData, err := json.MarshalIndent(result.ImportFile, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling import file for stack %s: %v\n", stack.PulumiStack, err)
			os.Exit(1)
		}

		if err := os.WriteFile(outputPath, outputData, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing import file for stack %s: %v\n", stack.PulumiStack, err)
			os.Exit(1)
		}

		// Update the import-resolved-file path in the migration file
		stack.ImportResolvedFile = outputPath
		needsSave = true
	}

	// Save the migration file if we updated any paths
	if needsSave {
		if err := mf.Save(migrationFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving migration file: %v\n", err)
			os.Exit(1)
		}
	}

	// If there were errors, report them
	if len(resolveErrors) > 0 {
		fmt.Println("Failed to resolve import stub files for one or more stacks:")
		fmt.Println()
		for _, rerr := range resolveErrors {
			fmt.Println()
			fmt.Printf("Stack %q:\n", rerr.stackName)
			fmt.Println()
			fmt.Printf("  Command: %s\n", rerr.command)
			fmt.Printf("  Error: %v\n", rerr.err)
			fmt.Println()
		}
		fmt.Println("Please fix the issues above before proceeding.")
		return false
	}

	return true
}

func ensureSourceCodeMapped(ctx context.Context, mf *tfmig.MigrationFile, mfPath string) bool {
	// Pick a stack to work with: "default" or alphabetically first
	var selectedStack *tfmig.Stack
	var selectedStackName string

	for i := range mf.Migration.Stacks {
		stack := &mf.Migration.Stacks[i]
		if stack.PulumiStack == "default" {
			selectedStack = stack
			selectedStackName = stack.PulumiStack
			break
		}
	}

	// If no "default", pick the first alphabetically
	if selectedStack == nil {
		stackNames := make([]string, 0, len(mf.Migration.Stacks))
		for _, stack := range mf.Migration.Stacks {
			stackNames = append(stackNames, stack.PulumiStack)
		}
		sort.Strings(stackNames)

		if len(stackNames) > 0 {
			selectedStackName = stackNames[0]
			for i := range mf.Migration.Stacks {
				if mf.Migration.Stacks[i].PulumiStack == selectedStackName {
					selectedStack = &mf.Migration.Stacks[i]
					break
				}
			}
		}
	}

	if selectedStack == nil {
		fmt.Fprintf(os.Stderr, "Error: No stacks found in migration file\n")
		os.Exit(1)
	}

	// Create a workspace and stack
	ws, err := auto.NewLocalWorkspace(ctx, auto.WorkDir(mf.Migration.PulumiSources))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating Pulumi workspace: %v\n", err)
		os.Exit(1)
	}

	stack, err := auto.SelectStack(ctx, selectedStackName, ws)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error selecting stack %s: %v\n", selectedStackName, err)
		os.Exit(1)
	}

	// Create a temporary file for import-stub.json
	tmpFile, err := os.CreateTemp("", "import-stub-*.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temporary file: %v\n", err)
		os.Exit(1)
	}
	importStubPath := tmpFile.Name()
	tmpFile.Close()
	defer func() {
		os.Remove(importStubPath)
	}()

	// Run preview with import file to generate import-stub.json
	previewResult, err := stack.Preview(ctx, optpreview.ImportFile(importStubPath))
	if err != nil {
		fmt.Println("Failed to run `pulumi preview --import-file import-stub.json`:")
		fmt.Println()
		fmt.Println(previewResult.StdErr)
		fmt.Println(previewResult.StdOut)
		fmt.Printf("%v\n", err)
		fmt.Println()
		fmt.Println("Please fix the issues above before proceeding.")
		return false
	}

	// Load import-stub.json
	importStubData, err := os.ReadFile(importStubPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading import-stub.json: %v\n", err)
		os.Exit(1)
	}

	var importStubs tfmig.ImportFile
	if err := json.Unmarshal(importStubData, &importStubs); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing import-stub.json: %v\n", err)
		os.Exit(1)
	}

	// Find missing resources by comparing import-stub.json with migration.json
	missingResources := findMissingResources(selectedStack, &importStubs)

	if len(missingResources) > 0 {
		fmt.Println("The next step is to ensure that every Terraform resource is translated to Pulumi and tracked in `migrations.json`.")
		fmt.Println()
		fmt.Printf("  Missing resources: %v\n", len(missingResources))
		fmt.Println()
		fmt.Println("If you have not yet started translating Terraform source code to Pulumi, do it now and try again.")
		fmt.Println()
		fmt.Printf("  Terraform: %v\n", mf.Migration.TFSources)
		fmt.Printf("  Pulumi:    %v\n", mf.Migration.PulumiSources)
		fmt.Println()

		// Pick the minimum one by address
		minResource := missingResources[0]
		for _, res := range missingResources {
			if res.TFAddr < minResource.TFAddr {
				minResource = res
			}
		}

		fmt.Println("Otherwise the next step is to iterate on the missing resources until none are left.")
		fmt.Println()
		fmt.Printf("The first of the %d missing resources:\n", len(missingResources))
		fmt.Println()
		fmt.Printf("  Terraform address:                      %v\n", minResource.TFAddr)
		if minResource.URN != "" {
			fmt.Printf("  Expected Pulumi URN in migrations.json: %v\n", minResource.URN)
		}
		fmt.Println()
		fmt.Printf("None of the %d resources in the Pulumi project seem to have this exact URN.\n",
			len(importStubs.Resources))
		fmt.Println()

		projectName, err := tfmig.ReadPulumiProjectName(mf.Migration.PulumiSources)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading Pulumi project name: %v\n", err)
			os.Exit(1)
		}

		if partials := findPartialMatches(minResource, &importStubs); len(partials) != 0 {
			fmt.Printf("Note there are %d similar Pulumi resources:\n", len(partials))
			for _, p := range partials {
				fmt.Printf("  - %v\n", tfmig.DeduceURN(projectName, selectedStackName, p))
			}
		}

		fmt.Println()
		fmt.Println("There are three options.")
		fmt.Println()
		fmt.Println("Option 1. If the resource has not been translated yet, translate it and add source code to the Pulumi project.")
		fmt.Println()
		fmt.Println("Option 2. If it has been translated, migration.json is not tracking the Terraform-to-Pulumi association properly.")
		fmt.Println("Translation may have used a different name for the resource leading to the actual URN mismatching the tracked URN.")
		fmt.Println("To fix the association, set the correct URN of the Pulumi resource tracking the Terraform resource:")
		fmt.Println()
		fmt.Printf("  pulumi-terraform-migrate set-urn --migration %q --tf-addr %q --urn $urn\n", mfPath, minResource.TFAddr)
		fmt.Println()
		fmt.Println("Option 3. Explicitly skip this resource from being tracked by the migration process:")
		fmt.Println()
		fmt.Printf("  pulumi-terraform-migrate skip --migration %q %q\n\n", mfPath, minResource.TFAddr)
		return false
	}

	return true
}

func findPartialMatches(res tfmig.Resource, importStubs *tfmig.ImportFile) []*optimport.ImportResource {
	var matches []*optimport.ImportResource
	for _, importRes := range importStubs.Resources {
		ok := tfmig.IsPartialMatch(res, importRes)
		if ok {
			matches = append(matches, importRes)
		}
	}
	return matches
}

// findMissingResources finds resources in migration.json that don't have equivalents in import-stub.json
func findMissingResources(stack *tfmig.Stack, importStubs *tfmig.ImportFile) []tfmig.Resource {
	var missing []tfmig.Resource

	// Build a set of resources present in import-stub.json
	importStubSet := make(map[string]bool)
	for _, importRes := range importStubs.Resources {
		// Try to match against stack resources
		for _, stackRes := range stack.Resources {
			ok, err := tfmig.IsMatchingResource(stackRes, importRes)
			if err != nil {
				continue
			}
			if ok {
				importStubSet[stackRes.TFAddr] = true
			}
		}
	}

	// Find resources in migration.json that aren't in import-stub.json
	for _, res := range stack.Resources {
		if res.Migrate == tfmig.MigrateModeSkip {
			continue
		}
		// Skip resources without TFAddr
		if res.TFAddr == "" {
			continue
		}

		if !importStubSet[res.TFAddr] {
			missing = append(missing, res)
		}
	}

	return missing
}
