package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"text/template"

	"golang.org/x/crypto/ssh/terminal"
)

//go:generate go run tools/include.go
var (
	debug = debugger{log.New(ioutil.Discard, " * ", 0)}
)

// TODO(nils): document build-state.format.log and build-state.format.state

const (
	buildStateDefaultTemplate = `Name:  {{.Name}}     Key: {{.Key}}
State: {{.State}}
URL:   {{.URL}}
Date:  {{.DateAdded}}

   {{.Description}}
`
	buildStatusDefaultTemplate = `{{.ID}} {{.Message}}
   Successful: {{.Status.Successful}}, In Progress: {{.Status.InProgress}}, Failed: {{.Status.Failed}}
`
)

type debugger struct {
	*log.Logger
}

func (d debugger) DumpRequest(req *http.Request, body bool) {
	dump, err := httputil.DumpRequestOut(req, body)
	if err != nil {
		d.Printf("error dumping request: %v", err)
	}
	d.Printf("Request: %q\n", dump)
}

func main() {
	log.SetFlags(log.Lshortfile)

	var (
		displayLogFlag       = flag.Bool("log", false, "Display git log with build statistics")
		generateB64CredsFlag = flag.Bool("generate-creds", false, "Generate credentials")
		installFlag          = flag.Bool("install", false, "Run installer")
		proto                = flag.String("proto", "https", "The protocoll to use")
		debugFlag            = flag.Bool("debug", false, "Enable debug output")
		format               = flag.String("format", "", "Go text template, see manual for more info")
		formatJSON           = flag.Bool("json", false, "Format output as JSON")
	)
	flag.Parse()

	if *debugFlag {
		debug.SetFlags(0)
		debug.SetPrefix("==> ")
		debug.SetOutput(os.Stderr)
	}

	init := true
	if *generateB64CredsFlag || *installFlag {
		init = false
	}

	code := 0
	subcmd := newSubcommand(init, subcommand{
		proto:      *proto,
		format:     *format,
		formatJSON: *formatJSON,
	})

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
	proto        string
	formatJSON   bool
	format       string
}

func newSubcommand(init bool, s subcommand) *subcommand {
	sub := &s

	if !init {
		return sub
	}

	ta := newBasicAuthFromCredentials(mustGitConfig("build-state.auth.user"), mustGitConfig("build-state.auth.credentials"))
	stashURL, err := stashAPIURL(s.proto)
	logFatalOnError(err)

	sub.stashService = newStashService(stashURL, ta)
	return sub
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
	var tmp []interface{}

	logs, err := gitLogShort(flag.Arg(0))
	logFatalOnError(err)

	bs, err := s.stashService.BuildStats(logs)
	logFatalOnError(err)

	if s.format == "" {
		s.format = buildStatusDefaultTemplate
		if f := defaultGitConfig("build-state.format.log"); f != "" {
			s.format = f
		}
	}

	t, err := template.New("BuildState").Parse(s.format)
	logFatalOnError(err)

	for _, log := range logs {
		bsl := struct {
			ID      CommitID
			Message string
			Status  BuildStatusCommitStat
		}{
			ID:      log.id,
			Message: log.message,
			Status:  bs[log.id],
		}
		if s.formatJSON {
			tmp = append(tmp, bsl)
			continue
		}
		err = t.Execute(os.Stdout, bsl)
		logFatalOnError(err)
	}

	if s.formatJSON {
		out, err := json.MarshalIndent(tmp, "", "   ")
		logFatalOnError(err)
		fmt.Printf("%s\n", out)
	}
	return 0
}

func (s *subcommand) displayBuildState() int {
	if s.format == "" {
		s.format = buildStateDefaultTemplate
		if f := defaultGitConfig("build-state.format.state"); f != "" {
			s.format = f
		}
	}
	debug.Printf("Format: %q", s.format)
	debug.Printf("Git ref: %s", flag.Arg(0))

	commit, err := newCommitIDFromRef(flag.Arg(0))
	if err != nil {
		if werr, ok := err.(*exec.ExitError); ok {
			log.Printf("%s", werr.Stderr)
		}
		log.Fatalf("Not a valid git reference: %s, error: %s", flag.Arg(0), err)
	}

	debug.Printf("Git commit: %s", commit)
	bs, err := s.stashService.BuildStatus(commit)
	logFatalOnError(err)

	if s.formatJSON {
		out, err := json.MarshalIndent(bs.Values, "", "   ")
		logFatalOnError(err)
		fmt.Printf("%s\n", out)
		return 0
	}

	fmt.Print(bs.Format(s.format))
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
		compDir = macBashCompletionDir()
	default:
		log.Fatalf("Unknown runtime: %s", runtime.GOOS)
	}

	if compDir != "" {
		compPath := path.Join(compDir, "git-build-state")
		fmt.Printf("Installing bash completion: %s\n", compPath)

		compFile, err := os.Create(compPath)
		logFatalOnError(err)
		defer compFile.Close()

		_, err = compFile.Write(asset("git-build-state"))
		logFatalOnError(err)
	}

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

func macBashCompletionDir() string {
	compDir := "/opt/local/etc/bash_completion.d"

	if _, err := os.Stat(compDir); err == nil {
		return compDir
	}

	brewPrefix, err := exec.Command("brew", "--prefix").Output()
	brewPrefix = bytes.TrimSpace(brewPrefix)
	if err != nil {
		return ""
	}
	return string(path.Join(string(brewPrefix), "etc/bash_completion.d"))
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
