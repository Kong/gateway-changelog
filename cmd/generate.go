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
	"sort"
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

func isYAML(filename string) bool {
	return strings.HasSuffix(filename, ".yml")
}

func fetchCommitContext(filename string) (ctx CommitContext, err error) {
	commit, err := utils.FindOriginalCommit("", filename)
	if err != nil {
		return
	}
	if debug {
		Debug("file %s original commit: %s", filename, commit)
	}

	prs, _, err := client.PullRequests.ListPullRequestsWithCommit(context.TODO(), options.GithubApiOwner, options.GithubApiRepo, commit, nil)
	if err != nil {
		return ctx, fmt.Errorf("failed to fetch pulls: %v", err)
	}
	if len(prs) == 0 {
		return ctx, fmt.Errorf("PullReqeusts is empty")
	}

	// Filter to find only merged PRs, starting from the last one
	var mergedPR *github.PullRequest
	for i := len(prs) - 1; i >= 0; i-- {
		if prs[i].MergedAt != nil {
			mergedPR = prs[i]
			break
		}
	}

	if mergedPR == nil {
		return ctx, fmt.Errorf("no merged PR found for commit")
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
		return fmt.Errorf("faield to fetch commit ctx: %v", err)
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

func collectFromFolder(repoPath string, changelogPath string, maps map[string]map[string][]*ChangelogEntry) error {
	changelogPath = filepath.Join(repoPath, changelogPath)
	exists, err := utils.DirExists(changelogPath)
	if !exists {
		return err
	}

	files, err := os.ReadDir(changelogPath)
	if err != nil {
		return err
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

		content, err := os.ReadFile(filepath.Join(changelogPath, file.Name()))
		if err != nil {
			return err
		}

		Info("processing changelog file: %s (%d/%d)", file.Name(), i, total)

		// parse entry
		entry := &ChangelogEntry{}
		err = yaml.Unmarshal(content, entry)
		if err != nil {
			return fmt.Errorf("failed to unmarshal YAML from %s: %v", file.Name(), err)
		}

		entry.fileName = filepath.Join(changelogPath, file.Name())

		err = processEntry(entry)
		if err != nil {
			return fmt.Errorf("fialed to process entry: %v", err)
		}

		if maps[entry.Type] == nil {
			maps[entry.Type] = make(map[string][]*ChangelogEntry)
		}
		maps[entry.Type][entry.Scope] = append(maps[entry.Type][entry.Scope], entry)
	}

	return nil
}

func collect() (*TemplateData, error) {
	maps := make(map[string]map[string][]*ChangelogEntry)

	for _, path := range options.ChangelogPaths {
		err := collectFromFolder(options.RepoPath, path, maps)
		if err != nil {
			return nil, err
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

	return data, nil
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

	data, err := collect()
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
