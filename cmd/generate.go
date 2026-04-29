package cmd

import (
	"context"
	"embed"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/Kong/changelog/utils"
	"github.com/google/go-github/v56/github"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v3"
)

const (
	jiraBaseURL = "https://konghq.atlassian.net/browse/"
)

//go:generate cp -f ../changelog-markdown.tmpl changelog-markdown.tmpl
//go:embed changelog-markdown.tmpl
var changelogTmplFS embed.FS

type GenerateCmdOptions struct {
	Title string

	RepoPath string

	ChangelogPaths []string

	WithJiras bool

	GithubApiOwner string
	GithubApiRepo  string

	GithubIssueRepo string
}

// global vars
var (
	options GenerateCmdOptions

	ScopePriority = map[string]int{
		"Performance":   10,
		"Configuration": 20,
		"Core":          30,
		"PDK":           40,
		"Plugin":        50,
		"Admin API":     60,
		"Clustering":    70,
		"Default":       100, // default priority
	}
	client *github.Client
)

type CommitContext struct {
	SHA     string
	Message string
	PrCtx   PullRequestContext
}

type PullRequestContext struct {
	Number int
	Title  string
	Body   string
}

type MissingPullRequestError struct {
	CommitSHA string
}

func (e *MissingPullRequestError) Error() string {
	return fmt.Sprintf("no merged PR found for commit %s", e.CommitSHA)
}

type EntryProcessingFailure struct {
	FileName  string
	CommitSHA string
	Err       error
}

func (e *EntryProcessingFailure) Error() string {
	var missingPR *MissingPullRequestError
	if errors.As(e.Err, &missingPR) {
		return fmt.Sprintf("reason: missing merged PR\nfile: %s\ncommit: %s", e.FileName, missingPR.CommitSHA)
	}

	if e.CommitSHA == "" {
		return fmt.Sprintf("file: %s\nerror: %v", e.FileName, e.Err)
	}

	return fmt.Sprintf("file: %s\ncommit: %s\nerror: %v", e.FileName, e.CommitSHA, e.Err)
}

func (e *EntryProcessingFailure) Unwrap() error {
	return e.Err
}

func logEntryProcessingFailure(failure EntryProcessingFailure) {
	Error("skipping changelog entry: %s\n", strings.ReplaceAll(failure.Error(), "\n", "\n  "))
}

func logEntryProcessingSummary(failures []EntryProcessingFailure) {
	if len(failures) == 0 {
		return
	}

	entryNoun := "entries"
	if len(failures) == 1 {
		entryNoun = "entry"
	}

	Error("\nskipped %d changelog %s:\n", len(failures), entryNoun)
	for i, failure := range failures {
		Error("%d. %s\n", i+1, strings.ReplaceAll(failure.Error(), "\n", "\n   "))
	}
}

func entryProcessingFailureFromError(err error, fileName string) *EntryProcessingFailure {
	var failure *EntryProcessingFailure
	if errors.As(err, &failure) {
		if failure.FileName == "" {
			failure.FileName = fileName
		}
		if failure.CommitSHA == "" {
			var missingPR *MissingPullRequestError
			if errors.As(failure.Err, &missingPR) {
				failure.CommitSHA = missingPR.CommitSHA
			}
		}
		return failure
	}

	var missingPR *MissingPullRequestError
	if !errors.As(err, &missingPR) {
		return nil
	}

	return &EntryProcessingFailure{
		FileName:  fileName,
		CommitSHA: missingPR.CommitSHA,
		Err:       err,
	}
}

var pullRequestRefPattern = regexp.MustCompile(`\(#(\d+)\)`)

func isYAML(filename string) bool {
	return strings.HasSuffix(filename, ".yml")
}

func findMergedPullRequest(prs []*github.PullRequest) *github.PullRequest {
	for i := len(prs) - 1; i >= 0; i-- {
		if prs[i].MergedAt != nil {
			return prs[i]
		}
	}

	return nil
}

func fetchMergedPullRequestFromCommitMessage(commit string) (*github.PullRequest, error) {
	repoCommit, _, err := client.Repositories.GetCommit(context.TODO(), options.GithubApiOwner, options.GithubApiRepo, commit, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to read commit message for %s: %v", commit, err)
	}

	matches := pullRequestRefPattern.FindAllStringSubmatch(repoCommit.GetCommit().GetMessage(), -1)
	if len(matches) == 0 {
		return nil, nil
	}

	seen := make(map[int]struct{}, len(matches))
	for i := len(matches) - 1; i >= 0; i-- {
		prNumber, err := strconv.Atoi(matches[i][1])
		if err != nil {
			continue
		}

		if _, ok := seen[prNumber]; ok {
			continue
		}
		seen[prNumber] = struct{}{}

		pr, resp, err := client.PullRequests.Get(context.TODO(), options.GithubApiOwner, options.GithubApiRepo, prNumber)
		if err != nil {
			if debug && (resp == nil || resp.StatusCode != http.StatusNotFound) {
				Debug("failed to fetch PR #%d for commit %s: %v", prNumber, commit, err)
			}
			continue
		}
		if pr.MergedAt == nil {
			continue
		}
		if pr.GetMergeCommitSHA() == commit {
			return pr, nil
		}
	}

	return nil, nil
}

func fetchCommitContext(filename string) (ctx CommitContext, err error) {
	commit, err := utils.FindOriginalCommit("", filename)
	if err != nil {
		return
	}
	ctx.SHA = commit
	if debug {
		Debug("file %s original commit: %s", filename, commit)
	}

	prs, _, err := client.PullRequests.ListPullRequestsWithCommit(context.TODO(), options.GithubApiOwner, options.GithubApiRepo, commit, nil)
	if err != nil {
		return ctx, fmt.Errorf("failed to fetch pulls for commit %s: %v", commit, err)
	}

	mergedPR := findMergedPullRequest(prs)
	if mergedPR == nil {
		mergedPR, err = fetchMergedPullRequestFromCommitMessage(commit)
		if err != nil {
			return ctx, fmt.Errorf("failed to resolve merged PR from commit message: %v", err)
		}
		if debug && mergedPR != nil {
			Debug("resolved merged PR #%d from commit message for %s", mergedPR.GetNumber(), commit)
		}
	}

	if mergedPR == nil {
		return ctx, &MissingPullRequestError{CommitSHA: commit}
	}

	ctx.PrCtx = PullRequestContext{
		Number: mergedPR.GetNumber(),
		Title:  mergedPR.GetTitle(),
		Body:   mergedPR.GetBody(),
	}

	return ctx, nil
}

type ScopeEntries struct {
	ScopeName string
	Entries   []*ChangelogEntry
}

type TemplateData struct {
	Title string
	Type  map[string][]ScopeEntries
}

type Jira struct {
	ID   string
	Link string
}

type Github struct {
	Name string
	Link string
}

type ChangelogEntry struct {
	Message       string   `yaml:"message"`
	Type          string   `yaml:"type"`
	Scope         string   `yaml:"scope"`
	Prs           []int    `yaml:"prs"`
	Githubs       []int    `yaml:"githubs"`
	Jiras         []string `yaml:"jiras"`
	ParsedJiras   []*Jira
	ParsedGithubs []*Github
	fileName      string
}

func parseGithub(githubNos []int) []*Github {
	list := make([]*Github, 0)
	for _, no := range githubNos {
		github := &Github{
			Name: fmt.Sprintf("#%d", no),
			Link: fmt.Sprintf("https://github.com/%s/issues/%d", options.GithubIssueRepo, no),
		}
		list = append(list, github)
	}
	return list
}

// processEntry process a changelog entry
func processEntry(entry *ChangelogEntry) error {
	if entry.Scope == "" {
		entry.Scope = "Default"
	}

	ctx, err := fetchCommitContext(entry.fileName)
	if err != nil {
		var missingPR *MissingPullRequestError
		if !errors.As(err, &missingPR) {
			return fmt.Errorf("failed to fetch commit ctx: %v", err)
		}

		return &EntryProcessingFailure{
			FileName:  entry.fileName,
			CommitSHA: ctx.SHA,
			Err:       fmt.Errorf("failed to fetch commit ctx: %w", err),
		}
	}

	// jiras
	if len(entry.Jiras) == 0 {
		jiraMap := make(map[string]bool)
		jiras := utils.MatchJiras(ctx.PrCtx.Body)
		for _, jira := range jiras {
			if !jiraMap[jira] {
				entry.Jiras = append(entry.Jiras, jira)
				jiraMap[jira] = true
			}
		}
	}
	if options.WithJiras {
		for _, jiraId := range entry.Jiras {
			jira := Jira{
				ID:   jiraId,
				Link: jiraBaseURL + jiraId,
			}
			entry.ParsedJiras = append(entry.ParsedJiras, &jira)
		}
	}

	// githubs
	if len(entry.Githubs) == 0 {
		entry.Githubs = entry.Prs
	}
	if len(entry.Githubs) == 0 {
		entry.Githubs = append(entry.Githubs, ctx.PrCtx.Number)
	}

	entry.ParsedGithubs = parseGithub(entry.Githubs)

	return nil
}

func mapKeys(m map[string][]*ChangelogEntry) []string {
	keys := make([]string, 0)
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func collectFromFolder(repoPath string, changelogPath string, maps map[string]map[string][]*ChangelogEntry) ([]EntryProcessingFailure, error) {
	failures := make([]EntryProcessingFailure, 0)
	changelogPath = filepath.Join(repoPath, changelogPath)
	exists, err := utils.DirExists(changelogPath)
	if !exists {
		return failures, err
	}

	files, err := os.ReadDir(changelogPath)
	if err != nil {
		return failures, err
	}
	total := len(files)
	Info("reading files from folder %s", changelogPath)
	for i := 1; i <= total; i++ {
		file := files[i-1]
		if file.IsDir() {
			continue
		}

		if !isYAML(file.Name()) {
			Debug("Skipping file: %s (%d/%d)", file.Name(), i, total)
			continue
		}

		filePath := filepath.Join(changelogPath, file.Name())

		content, err := os.ReadFile(filePath)
		if err != nil {
			return failures, err
		}

		Info("processing changelog file: %s (%d/%d)", file.Name(), i, total)

		// parse entry
		entry := &ChangelogEntry{}
		err = yaml.Unmarshal(content, entry)
		if err != nil {
			return failures, fmt.Errorf("failed to unmarshal YAML from %s: %v", file.Name(), err)
		}

		entry.fileName = filePath

		err = processEntry(entry)
		if err != nil {
			failure := entryProcessingFailureFromError(err, filePath)
			if failure == nil {
				return failures, fmt.Errorf("failed to process entry: %v", err)
			}

			logEntryProcessingFailure(*failure)
			failures = append(failures, *failure)
			continue
		}

		if maps[entry.Type] == nil {
			maps[entry.Type] = make(map[string][]*ChangelogEntry)
		}
		maps[entry.Type][entry.Scope] = append(maps[entry.Type][entry.Scope], entry)
	}

	return failures, nil
}

func collect() (*TemplateData, []EntryProcessingFailure, error) {
	maps := make(map[string]map[string][]*ChangelogEntry)
	failures := make([]EntryProcessingFailure, 0)

	for _, path := range options.ChangelogPaths {
		folderFailures, err := collectFromFolder(options.RepoPath, path, maps)
		failures = append(failures, folderFailures...)
		if err != nil {
			return nil, failures, err
		}
	}

	data := &TemplateData{
		Type: make(map[string][]ScopeEntries),
	}

	//data.Type = make(map[string][]ScopeEntries)
	for t, scopeEntries := range maps {
		scopes := mapKeys(scopeEntries)
		sort.Slice(scopes, func(i, j int) bool {
			scopei := scopes[i]
			scopej := scopes[j]
			return ScopePriority[scopei] < ScopePriority[scopej]
		})

		list := make([]ScopeEntries, 0)
		for _, scope := range scopes {
			entries := ScopeEntries{
				ScopeName: scope,
				Entries:   scopeEntries[scope],
			}
			list = append(list, entries)
		}
		data.Type[t] = list
	}

	return data, failures, nil
}

func generate(data *TemplateData) error {
	tmpl, err := template.New("changelog-markdown.tmpl").Funcs(template.FuncMap{
		"arr": func(values ...any) []any { return values },
		"dict": func(values ...any) (map[string]any, error) {
			if len(values)%2 != 0 {
				return nil, errors.New("invalid dictionary call")
			}

			root := make(map[string]any)

			for i := 0; i < len(values); i += 2 {
				dict := root
				var key string
				switch v := values[i].(type) {
				case string:
					key = v
				case []string:
					for i := 0; i < len(v)-1; i++ {
						key = v[i]
						var m map[string]any
						v, found := dict[key]
						if found {
							m = v.(map[string]any)
						} else {
							m = make(map[string]any)
							dict[key] = m
						}
						dict = m
					}
					key = v[len(v)-1]
				default:
					return nil, errors.New("invalid dictionary key")
				}
				dict[key] = values[i+1]
			}

			return root, nil
		},
		"trim": func(value string) string {
			return strings.TrimSpace(value)
		},
	}).ParseFS(changelogTmplFS, "changelog-markdown.tmpl")
	if err != nil {
		return err
	}
	err = tmpl.Execute(os.Stdout, data)
	if err != nil {
		return err
	}
	return nil
}

// Generate output the changelog
func Generate() error {
	Debug("Options: %+v", options)

	data, failures, err := collect()
	logEntryProcessingSummary(failures)
	if err != nil {
		return err
	}

	data.Title = options.Title
	return generate(data)
}

func newGenerateCmd() *cli.Command {
	cmd := &cli.Command{
		Name:        "generate",
		Description: "The generate command output the generated changelog markdown to /dev/stdout",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "repo-path",
				Usage:    "The repository path (/path/to/your/repository)",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "changelog-path",
				Usage:    "The changelog folder relative path (changelog/unreleased/kong)",
				Required: false,
			},
			&cli.StringFlag{
				Name:     "title",
				Usage:    "The title name (Kong)",
				Required: true,
			},
			&cli.StringSliceFlag{
				Name:     "changelog-paths",
				Usage:    "The changelog folder relative paths (changelog/unreleased/kong)",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "github-issue-repo",
				Usage:    "The repo name that is used to compose the GitHub issue link. (OWNER/REPO)",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "github-api-repo",
				Usage:    "The repo name that is used to compose the GitHub URL to retrieve data. (OWNER/REPO)",
				Required: true,
			},
			&cli.BoolFlag{
				Name:     "with-jiras",
				Usage:    "Display Jira links",
				Required: false,
			},
		},
		Action: func(c *cli.Context) error {
			githubToken := os.Getenv("GITHUB_TOKEN")
			if githubToken == "" {
				return errors.New("environment variable GITHUB_TOKEN is required")
			}

			httpClient := &http.Client{}
			if debug {
				httpClient.Transport = &LoggingTransport{
					Transport: http.DefaultTransport,
				}
			}
			client = github.NewClient(httpClient).WithAuthToken(githubToken)

			parts := strings.Split(c.String("github-api-repo"), "/")

			options = GenerateCmdOptions{
				RepoPath:        c.String("repo-path"),
				ChangelogPaths:  c.StringSlice("changelog-paths"),
				Title:           c.String("title"),
				GithubApiOwner:  parts[0],
				GithubApiRepo:   parts[1],
				GithubIssueRepo: c.String("github-issue-repo"),
				WithJiras:       c.Bool("with-jiras"),
			}

			return Generate()
		},
	}

	return cmd
}
