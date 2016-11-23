package main

import (
	"bytes"
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"strings"
	"syscall"
)

type shortLogEntry struct {
	id      CommitID
	message string
}

type shortLog []shortLogEntry

func (sh shortLog) CommitIDs() CommitIDs {
	var log CommitIDs
	for _, entry := range sh {
		log = append(log, CommitID(entry.id))
	}
	return log
}

func gitLogShort(branch string) (shortLog, error) {
	if branch == "" {
		b, err := gitCurrentBranch()
		if err != nil {
			return nil, err
		}
		branch = b
	}

	var logs shortLog
	cmd := exec.Command("git", "log", "--pretty=oneline", branch)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := bytes.Split(output, []byte("\n"))
	for _, line := range lines {
		parts := bytes.SplitN(line, []byte(" "), 2)
		if len(parts) != 2 {
			continue
		}
		logs = append(logs, shortLogEntry{id: CommitID(parts[0]), message: string(parts[1])})
	}
	return logs, nil
}

func gitCurrentBranch() (string, error) {
	output, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	output = bytes.TrimSpace(output)
	return string(output), err
}

func gitRemote() (*url.URL, error) {
	output, err := exec.Command("git", "remote", "-v").Output()
	if err != nil {
		return nil, err
	}

	lines := bytes.Split(output, []byte("\n"))
	if len(lines) == 0 {
		return nil, fmt.Errorf("No output when trying to fetch git remote")
	}
	parts := strings.Fields(string(lines[0]))
	remote, err := url.Parse(string(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("Unable to parse remote: %v", err)
	}

	return remote, nil
}

func gitConfig(key string) (string, error) {
	output, err := exec.Command("git", "config", key).Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func setGitConfig(key, value string) error {
	return exec.Command("git", "config", "--global", key, value).Run()
}

// mustGitConfig returns the given configuration value for the key. All errors
// will terminate the execution.
func mustGitConfig(key string) string {
	value, err := gitConfig(key)
	if err != nil {
		log.Fatalf("Unable to fetch git config %s: %v", key, err)
	}
	return value
}

// defaultGitConfig is a special case of mustGitConfig. It returns the given
// configuration value for the key. If the key does not exist in the
// configuration an empty string will be returned. All other errors will abort
// execution.
func defaultGitConfig(key string) string {
	value, err := gitConfig(key)
	if err != nil {
		if exitCode(err) == 1 {
			return ""
		}
		log.Fatalf("Unable to fetch git config %s: %v", key, err)
	}
	return value
}

func exitCode(err error) int {
	if exiterr, ok := err.(*exec.ExitError); ok {
		if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	return 0
}
