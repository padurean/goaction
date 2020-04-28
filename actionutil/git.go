package actionutil

import (
	"fmt"
	"log"
	"os"

	"github.com/posener/goaction"
	"github.com/posener/script"
)

// GitConfig configures git with name and email to enable git operations.
func GitConfig(name, email string) error {
	err := git("config", "user.name", name).ToStdout()
	if err != nil {
		return err
	}
	return git("config", "user.email", email).ToStdout()
}

// GitDiff returns diff of changes in a given file. The file will be staged after running this
// command.
func GitDiff(path string) (string, error) {
	// Add files to git, in case it does not exists
	err := git("add", path).ToStdout()
	defer func() { git("reset", path).ToStdout() }()
	if err != nil {
		return "", fmt.Errorf("git add for %s: %s", path, err)
	}
	return git("diff", "--staged", "--no-color", path).Tail(-5).ToString()
}

// GitCommitPush commits and pushes a list of files.
func GitCommitPush(paths []string, message string) error {
	branch := goaction.Branch()

	// Reset git if there where any staged files.
	err := git("reset").ToStdout()
	if err != nil {
		return fmt.Errorf("git reset: %s", err)
	}

	// Add the requested paths.
	err = git("add", paths...).ToStdout()
	if err != nil {
		return fmt.Errorf("git add: %s", err)
	}

	// Commit the changes.
	err = git("commit", "-m", message).ToStdout()
	if err != nil {
		return fmt.Errorf("git commit: %s", err)
	}

	retry := 1
	maxRetries := 3
	for {
		// Push the change.
		err = git("push", "origin", "HEAD:"+branch).ToStdout()
		if err != nil && retry > maxRetries {
			return fmt.Errorf("push failed %d times: %s", maxRetries, err)
		}
		retry++
		// In case of push error, try to rebase and push again, in case the error was due to other
		// changes being pushed to the remote repository.
		log.Printf("Push failed, rebasing and trying again...")
		err = git("pull", "--rebase", "--autostash", "origin", branch).ToStdout()
		if err != nil {
			return fmt.Errorf("git pull rebase: %s", err)
		}
	}
}

func git(subcmd string, args ...string) script.Stream {
	args = append([]string{subcmd}, args...)
	return script.ExecHandleStderr(os.Stderr, "git", args...)
}