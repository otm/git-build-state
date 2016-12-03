package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
)

// StashService holds information regarding the stash service
type StashService struct {
	url           *url.URL
	authenticator Authenticator
}

func newStashService(URL *url.URL, a Authenticator) *StashService {
	return &StashService{
		url:           URL,
		authenticator: a,
	}
}

// Commits lists commits for a repository
func (s *StashService) Commits(project, repo string) CommitIDs {
	p := fmt.Sprintf("rest/api/1.0/projects/%s/repos/%s/commits", project, repo)
	client := &http.Client{}
	s.url.Path = path.Join(s.url.Path, p)
	req, _ := http.NewRequest("GET", s.url.String(), nil)
	req = s.authenticator.Auth(req)

	res, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := ioutil.ReadAll(res.Body)
	fmt.Printf("%s\n", body)
	return CommitIDs{}
}

// BuildStats lists status given commit ids
func (s *StashService) BuildStats(c CommitIDer) (BuildStatusCommitStats, error) {
	p := "/rest/build-status/1.0/commits/stats"
	client := &http.Client{}
	b, err := json.Marshal(c.CommitIDs())
	if err != nil {
		fmt.Printf("Error: %s", err)
		return nil, err
	}

	req, err := http.NewRequest("POST", s.url.String()+p, bytes.NewBuffer(b))
	if err != nil {
		return nil, err
	}
	req = s.authenticator.Auth(req)
	req.Header.Set("X-Atlassian-Token", "no-check")
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var commitStatus BuildStatusCommitStats
	err = json.Unmarshal(body, &commitStatus)
	if err != nil {
		return nil, newStashError(body)
	}

	return commitStatus, nil
}

// BuildStatus provides detailed information regarding the build
func (s *StashService) BuildStatus(c CommitID) (BuildStatusResponse, error) {
	p := fmt.Sprintf("/rest/build-status/1.0/commits/%s", c)
	client := &http.Client{}

	req, err := http.NewRequest("GET", s.url.String()+p, nil)
	logFatalOnError(err)
	req = s.authenticator.Auth(req)
	req.Header.Set("X-Atlassian-Token", "no-check")

	res, err := client.Do(req)
	logFatalOnError(err)
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	logFatalOnError(err)

	var buildStatus BuildStatusResponse
	err = json.Unmarshal(body, &buildStatus)
	if err != nil || buildStatus.Size == 0 {
		logFatalOnError(newStashError(body))
	}

	return buildStatus, nil
}

// CommitID is a string representation of the full commit id
type CommitID string

func newCommitIDFromRef(ref string) (CommitID, error) {
	if ref == "" {
		ref = "HEAD"
	}
	output, err := exec.Command("git", "show", "-q", "--pretty=format:%H", ref).Output()
	if err != nil {
		return "", err
	}

	return CommitID(strings.TrimSpace(string(output))), nil
}

func (cid CommitID) abbrevCommit() string {
	return string(cid[:7])
}

// CommitIDs is a list of commits
type CommitIDs []CommitID

// CommitIDer is an interface
type CommitIDer interface {
	CommitIDs() CommitIDs
}

// BuildStatusCommitStat holds information for a build
type BuildStatusCommitStat struct {
	Successful int `json:"successful"`
	InProgress int `json:"inProgress"`
	Failed     int `json:"failed"`
}

func (bs BuildStatusCommitStat) String() string {
	return fmt.Sprintf("Successful: %d, In Progress: %d, Failed: %d", bs.Successful, bs.InProgress, bs.Failed)
}

// BuildStatusCommitStats holds information for builds
type BuildStatusCommitStats map[CommitID]BuildStatusCommitStat

// BuildState is the state representation
type BuildState string

// StashTime is used for unmarshaling JSON
type StashTime time.Time

// UnmashalJSON decodes string to time.Time
func (st *StashTime) UnmashalJSON(b []byte) error {
	t, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return err
	}

	*st = StashTime(time.Unix(0, t*1000*1000))
	return nil
}

// BuildStatus hold the current iformation from stash
type BuildStatus struct {
	State       BuildState `json:"state"`
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	URL         string     `json:"url"`
	Description string     `json:"description"`
	DateAdded   StashTime  `json:"dateAdded"`
}

// Format the output of the BuildStatus
func (bs BuildStatus) Format(tmpl string) string {
	var buf bytes.Buffer
	t, err := template.New("BuildState").Parse(tmpl)
	logFatalOnError(err)
	err = t.Execute(&buf, bs)
	logFatalOnError(err)
	return buf.String()
}

func (bs BuildStatus) String() string {
	tmpl := `Name:  {{.Name}}     Key: {{.Key}}
State: {{.State}}
URL:   {{.URL}}
Date:  {{.Date}}

   {{.Description}}
`
	return bs.Format(tmpl)
}

// BuildStatusResponse represent the JSON response from stash
type BuildStatusResponse struct {
	Size       int           `json:"size"`
	Limit      int           `json:"limit"`
	IsLastPage bool          `json:"isLastPage"`
	Start      int           `json:"start"`
	Values     []BuildStatus `json:"values"`
}

// Format returns a BuildStatus formated according to tmpl which should
// be a valid text.Template string definition
func (bsr BuildStatusResponse) Format(tmpl string) string {
	var buf bytes.Buffer
	for _, value := range bsr.Values {
		buf.WriteString(value.Format(tmpl))
		buf.Write([]byte("\n"))
	}
	return buf.String()
}

func (bsr BuildStatusResponse) String() string {
	var buf bytes.Buffer
	for _, value := range bsr.Values {
		buf.WriteString(value.String())
		buf.Write([]byte("\n"))
	}
	return buf.String()
}

// StashError is used for unmashaling JSON errors from stash
type StashError struct {
	Errors []struct {
		Message       string `json:"message"`
		ExceptionName string `json:"exceptionName"`
	} `json:"errors"`
}

func newStashError(b []byte) error {
	var se StashError
	err := json.Unmarshal(b, &se)
	if err != nil {
		return err
	}

	return se
}
func (se StashError) Error() string {
	var buf bytes.Buffer
	for _, err := range se.Errors {
		if buf.Len() > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString(err.Message)
	}
	return buf.String()
}

// Authenticator interface provides a layer to autheticate services
type Authenticator interface {
	Auth(*http.Request) *http.Request
}

// TokenAuth implements the Authenticator interface
type TokenAuth struct {
	user  string
	token string
}

func newTokenAuth(user, token string) *TokenAuth {
	return &TokenAuth{
		user:  user,
		token: token,
	}
}

// Auth will add authentication headers to the request object
func (ta *TokenAuth) Auth(r *http.Request) *http.Request {
	r.Header.Set("X-Auth-User", ta.user)
	r.Header.Set("X-Auth-Token", ta.token)
	return r
}

// BasicAuth is used for username / password authentication
type BasicAuth struct {
	user           string
	b64credentials string
}

func newBasicAuth(user, password string) *BasicAuth {
	b64credentials := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
	return newBasicAuthFromCredentials(user, b64credentials)
}

func newBasicAuthFromCredentials(user, b64credentials string) *BasicAuth {
	return &BasicAuth{
		user:           user,
		b64credentials: b64credentials,
	}
}

// Auth will add authentication headers to the request object
func (ba *BasicAuth) Auth(r *http.Request) *http.Request {
	r.Header.Set("Authorization", "Basic "+ba.b64credentials)
	return r
}

func stashAPIURL() (*url.URL, error) {
	if endpoint := defaultGitConfig("build-state.endpoint"); endpoint != "" {
		return url.Parse(endpoint)
	}

	port := defaultGitConfig("build-state.port")

	remote, err := gitRemote()
	logFatalOnError(err)

	stashURL := remote.Host
	if i := strings.Index(stashURL, ":"); i != -1 {
		stashURL = stashURL[:i]
	}
	if port != "" {
		stashURL = stashURL + ":" + port
	}
	return url.Parse("https://" + stashURL)
}
