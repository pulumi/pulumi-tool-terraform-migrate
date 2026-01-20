// Copyright 2016-2026, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package providermap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Used in internal offline tooling only.
func FetchReleaseVersions(bp BridgedProvider) []ReleaseTag {
	tmpDir, err := os.MkdirTemp("", "git-ls-remote-*")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(tmpDir)

	remote := gitRemote(bp)
	cmd := exec.Command("git", "ls-remote", "--tags", remote)
	cmd.Dir = tmpDir
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	tagsBySha := parseLsRemoteTags(string(output))

	var tags []ReleaseTag
	for _, shaTags := range tagsBySha {
		tags = append(tags, shaTags...)
	}

	// Sort tags newest-first by semver
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Semver().GT(tags[j].Semver())
	})

	return tags
}

// Used in internal offline tooling only.
func InferUpstreamVersion(bp BridgedProvider, tag ReleaseTag) (ReleaseTag, error) {
	// Use deterministic cache folder for this provider
	cacheBase := filepath.Join(os.TempDir(), ".pulumi-bridged-providers")
	cacheDir := filepath.Join(cacheBase, fmt.Sprintf("pulumi-%s", bp))
	repoDir := filepath.Join(cacheDir, "repo")

	// Check if repo already exists
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		// Create cache directory if it doesn't exist
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create cache dir: %w", err)
		}

		// Clone the pulumi provider at the specified tag
		remote := gitRemote(bp)
		cloneCmd := exec.Command("git", "clone", "--branch", string(tag), remote, "repo")
		cloneCmd.Stdout = os.Stdout
		cloneCmd.Stderr = os.Stderr
		cloneCmd.Dir = cacheDir
		if err := cloneCmd.Run(); err != nil {
			return "", fmt.Errorf("failed to clone %s at %s: %w", remote, tag, err)
		}
	} else {
		// Repo exists, fetch the specific tag
		fetchCmd := exec.Command("git", "fetch", "origin", string(tag))
		fetchCmd.Stdout = os.Stdout
		fetchCmd.Stderr = os.Stderr
		fetchCmd.Dir = repoDir
		if err := fetchCmd.Run(); err != nil {
			return "", fmt.Errorf("failed to fetch %s: %w", tag, err)
		}

		// Checkout the tag
		checkoutCmd := exec.Command("git", "checkout", string(tag))
		checkoutCmd.Stdout = os.Stdout
		checkoutCmd.Stderr = os.Stderr
		checkoutCmd.Dir = repoDir
		if err := checkoutCmd.Run(); err != nil {
			return "", fmt.Errorf("failed to checkout %s: %w", tag, err)
		}
	}

	tag, err := inferUpstreamVersionFromSubmodule(repoDir, cacheDir)
	if err != nil {
		// Fall back to commit message parsing.
		return inferUpstreamVersionFromCommitMsg(repoDir)
	}
	return tag, nil
}

func inferUpstreamVersionFromCommitMsg(repoDir string) (ReleaseTag, error) {
	// Get the current commit message
	cmd := exec.Command("git", "log", "-1", "--format=%B")
	cmd.Dir = repoDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get commit message: %w", err)
	}

	commitMsg := strings.TrimSpace(string(output))
	version, err := parseVersionFromCommitMsg(commitMsg)
	if err != nil {
		return "", fmt.Errorf("failed to parse version from commit message: %w", err)
	}

	return ReleaseTag(version), nil
}

func inferUpstreamVersionFromSubmodule(repoDir, cacheDir string) (ReleaseTag, error) {
	// Get the submodule commit SHA from git ls-tree
	lsTreeCmd := exec.Command("git", "ls-tree", "HEAD", "upstream")
	lsTreeCmd.Dir = repoDir
	lsTreeOutput, err := lsTreeCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get upstream submodule info: %w", err)
	}

	// Parse ls-tree output: "160000 commit <sha>\tupstream"
	lsTreeLine := strings.TrimSpace(string(lsTreeOutput))
	if lsTreeLine == "" {
		return "", fmt.Errorf("no upstream submodule found")
	}
	lsTreeParts := strings.Fields(lsTreeLine)
	if len(lsTreeParts) < 3 {
		return "", fmt.Errorf("unexpected ls-tree output: %s", lsTreeLine)
	}
	submoduleCommit := lsTreeParts[2]

	// Read .gitmodules to find the upstream URL
	gitmodulesPath := repoDir + "/.gitmodules"
	gitmodulesContent, err := os.ReadFile(gitmodulesPath)
	if err != nil {
		return "", fmt.Errorf("failed to read .gitmodules: %w", err)
	}

	upstreamURL := parseUpstreamURL(string(gitmodulesContent))
	if upstreamURL == "" {
		return "", fmt.Errorf("could not find upstream submodule URL in .gitmodules")
	}

	// Fetch all tags from the upstream repository
	lsRemoteCmd := exec.Command("git", "ls-remote", "--tags", upstreamURL)
	lsRemoteCmd.Dir = cacheDir
	lsRemoteOutput, err := lsRemoteCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to ls-remote upstream %s: %w", upstreamURL, err)
	}

	tagsBySha := parseLsRemoteTags(string(lsRemoteOutput))
	matchingTags := tagsBySha[submoduleCommit]

	if len(matchingTags) == 0 {
		return "", fmt.Errorf("no matching tag found for commit %s", submoduleCommit)
	}
	if len(matchingTags) > 1 {
		return "", fmt.Errorf("multiple matching tags found: %v", matchingTags)
	}

	return matchingTags[0], nil
}

func gitRemote(bp BridgedProvider) string {
	return fmt.Sprintf("https://github.com/pulumi/pulumi-%s", bp)
}

// parseLsRemoteTags parses `git ls-remote --tags` output and returns a map
// from commit SHA to stable version tags (vX.Y.Z format) pointing to that commit.
func parseLsRemoteTags(output string) map[string][]ReleaseTag {
	stableTagPattern := regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	result := make(map[string][]ReleaseTag)

	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		// Line format: <sha>\trefs/tags/<tag>
		parts := strings.Split(line, "\t")
		if len(parts) != 2 {
			continue
		}
		sha := parts[0]
		tag := strings.TrimPrefix(parts[1], "refs/tags/")
		// Skip dereferenced tags (^{})
		if strings.HasSuffix(tag, "^{}") {
			continue
		}
		if stableTagPattern.MatchString(tag) {
			result[sha] = append(result[sha], ReleaseTag(tag))
		}
	}
	return result
}

func parseUpstreamURL(gitmodules string) string {
	// Parse .gitmodules to find the URL for the [submodule "upstream"] section
	lines := strings.Split(gitmodules, "\n")
	inUpstreamSection := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[submodule") && strings.Contains(line, "upstream") {
			inUpstreamSection = true
			continue
		}
		if strings.HasPrefix(line, "[") {
			inUpstreamSection = false
			continue
		}
		if inUpstreamSection && strings.HasPrefix(line, "url") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}
