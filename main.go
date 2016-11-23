package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"

	"golang.org/x/crypto/ssh/terminal"
)

//go:generate go run tools/include.go

func main() {
	log.SetFlags(log.Lshortfile)

	displayLogFlag := flag.Bool("log", false, "Display git log with build statistics")
	generateB64CredsFlag := flag.Bool("generate-creds", false, "Generate credentials")
	installFlag := flag.Bool("install", false, "Run installer")
	flag.Parse()

	init := true
	if *generateB64CredsFlag || *installFlag {
		init = false
	}

	code := 0
	subcmd := newSubcommand(init)

	switch {
	case *generateB64CredsFlag:
		code = subcmd.generateB64Credentials()
	case *installFlag:
		code = subcmd.install()
	case *displayLogFlag:
		code = subcmd.displayLog()
	default:
		code = subcmd.displayBuildState()
	}

	os.Exit(code)

}

type subcommand struct {
	stashService *StashService
}

func newSubcommand(init bool) *subcommand {
	s := &subcommand{}

	if !init {
		return s
	}

	// ta := newTokenAuth(mustGitConfig("build-state.auth.user"), mustGitConfig("build-state.auth.token"))
	ta := newBasicAuthFromCredentials(mustGitConfig("build-state.auth.user"), mustGitConfig("build-state.auth.credentials"))
	stashURL, err := stashAPIURL()
	logFatalOnError(err)

	s.stashService = newStashService(stashURL, ta)
	return s
}

func (s *subcommand) generateB64Credentials() int {
	_, _, b64credentials := readUserAndPassword()
	fmt.Printf("git config --global build-state.auth.credentials %s\n", b64credentials)
	return 0
}

func readUserAndPassword() (user, password, b64credentials string) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Username: ")
	user, err := reader.ReadString('\n')
	logFatalOnError(err)

	fmt.Print("Password: ")
	passwd, err := terminal.ReadPassword(int(os.Stdin.Fd()))
	logFatalOnError(err)

	// Print a new line as the we did not echo when reading the password
	fmt.Println("")

	user = strings.TrimSpace(user)
	passwd = bytes.TrimSpace(passwd)
	ta := newBasicAuth(user, string(passwd))
	return user, string(passwd), ta.b64credentials
}

func (s *subcommand) displayLog() int {
	logs, err := gitLogShort(flag.Arg(0))
	logFatalOnError(err)

	bs, err := s.stashService.BuildStats(logs)
	logFatalOnError(err)

	for _, log := range logs {
		fmt.Printf("%s %s\n", log.id, log.message)
		fmt.Printf("   %s\n", bs[log.id])
		fmt.Printf("\n")
	}
	return 0
}

func (s *subcommand) displayBuildState() int {
	commit, err := newCommitIDFromRef(flag.Arg(0))
	if err != nil {
		if werr, ok := err.(*exec.ExitError); ok {
			log.Printf("%s", werr.Stderr)
		}
		log.Fatalf("Not a valid git reference: %s, error: %s", flag.Arg(0), err)
	}
	bs, err := s.stashService.BuildStatus(commit)
	logFatalOnError(err)
	fmt.Print(bs)
	return 0
}

func (s *subcommand) install() int {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		fmt.Println("There is only support for installation in Linux and MacOS")
		return 1
	}

	// Install man pages
	manPath := "/usr/local/share/man/man1/git-build-state.1"
	fmt.Printf("Installing man page: %s\n", manPath)
	manFile, err := os.Create(manPath)
	logFatalOnError(err)
	_, err = manFile.Write(asset("git-build-state.1"))
	logFatalOnError(err)
	err = manFile.Close()
	logFatalOnError(err)

	// Install bash completion
	compDir := ""
	switch runtime.GOOS {
	case "linux":
		compDir = "/etc/bash_completion.d"
	case "darwin":
		compDir = "/opt/local/etc/bash_completion.d"
	default:
		log.Fatalf("Unknown runtime: %s", runtime.GOOS)
	}
	compPath := path.Join(compDir, "git-build-state")
	fmt.Printf("Installing bash completion: %s\n", compPath)

	compFile, err := os.Create(compPath)
	logFatalOnError(err)
	_, err = compFile.Write(asset("git-build-state"))
	logFatalOnError(err)
	err = compFile.Close()
	logFatalOnError(err)

	// Configure user
	fmt.Println("\nConfiguring Stash/Bitbucket credentials (abort with ctrl-c)")
	fmt.Println("Base64 encoded password will be saved in global git config")
	user, _, b64credentials := readUserAndPassword()
	fmt.Printf("Setting: %s=%s\n", "build-state.auth.user", user)
	logFatalOnError(setGitConfig("build-state.auth.user", user))

	fmt.Printf("Setting: %s=%s\n", "build-state.auth.credentials", "**********")
	logFatalOnError(setGitConfig("build-state.auth.credentials", b64credentials))

	return 0
}

func logFatalOnError(err error) {
	if err == nil {
		return
	}

	// reset log flags
	log.SetFlags(0)

	// get caller
	_, file, line, _ := runtime.Caller(1)
	file = path.Base(file)

	if werr, ok := err.(*exec.ExitError); ok {
		log.Printf("%s:%d: %s", file, line, werr.Stderr)
	}

	log.Fatalf("%s:%d: %v\n", file, line, err)
}
