package main

import (
    "encoding/json"
    "errors"
    "fmt"
    "github.com/urfave/cli/v2"
    "gopkg.in/yaml.v3"
    "io"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "regexp"
    "sort"
    "strings"
    "text/template"
)

const (
    JiraBaseUrl = "https://konghq.atlassian.net/browse/"
)

var (
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
    repoPath      string
    changelogPath string
    system        string
    repo          string
    token         string
)

type CommitCtx struct {
    SHA     string
    Message string
}

type PullCtx struct {
    Number int
    Title  string
    Body   string
}

type CommitContext struct {
    Commit  CommitCtx
    PullCtx PullCtx
}

func isYAML(filename string) bool {
    return strings.HasSuffix(filename, ".yml")
}

func fetchCommitContext(filename string) (*CommitContext, error) {
    ctx := &CommitContext{}
    filename = filepath.Join(changelogPath, filename)

    client := &http.Client{}

    req, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/commits?path=%s", repo, filename), nil)
    if len(token) > 0 {
        req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
    }
    response, err := client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("failed to fetch commits: %v", err)
    }
    defer response.Body.Close()
    if response.StatusCode != 200 {
        return nil, fmt.Errorf("failed to fetch commits: %d %s", response.StatusCode, response.Status)
    }

    bytes, err := io.ReadAll(response.Body)
    if err != nil {
        return nil, err
    }

    var res []map[string]interface{}
    err = json.Unmarshal(bytes, &res)
    if err != nil {
        return nil, fmt.Errorf("failed unmarshal: %v", err)
    }

    ctx.Commit = CommitCtx{
        SHA:     res[0]["sha"].(string),
        Message: res[0]["commit"].(map[string]interface{})["message"].(string),
    }

    req, err = http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/commits/%s/pulls", repo, ctx.Commit.SHA), nil)
    if len(token) > 0 {
        req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
    }
    response, err = client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("failed to fetch pulls: %v", err)
    }
    defer response.Body.Close()

    if response.StatusCode != 200 {
        return nil, fmt.Errorf("failed to fetch pulls: %d %s", response.StatusCode, response.Status)
    }

    bytes, err = io.ReadAll(response.Body)
    if err != nil {
        return nil, err
    }
    err = json.Unmarshal(bytes, &res)
    if err != nil {
        return nil, fmt.Errorf("failed unmarshal: %v", err)
    }
    ctx.PullCtx = PullCtx{
        Number: int(res[len(res)-1]["number"].(float64)),
        Title:  res[len(res)-1]["title"].(string),
        Body:   res[len(res)-1]["body"].(string),
    }

    return ctx, nil
}

func matchPattern(text, pattern string, t *[]string) {
    re := regexp.MustCompile(pattern)
    matches := re.FindAllString(text, -1)
    *t = append(*t, matches...)
}

type ScopeEntries struct {
    ScopeName string
    Entries   []*ChangelogEntry
}

type Data struct {
    System string
    Type   map[string][]ScopeEntries
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
}

func parseGithub(githubNos []int) []*Github {
    list := make([]*Github, 0)
    for _, no := range githubNos {
        github := &Github{
            Name: fmt.Sprintf("#%d", no),
            Link: fmt.Sprintf("https://github.com/%s/issues/%d", repo, no),
        }
        list = append(list, github)
    }
    return list
}

func processEntry(filename string, entry *ChangelogEntry) error {
    if entry.Scope == "" {
        entry.Scope = "Default"
    }

    ctx, err := fetchCommitContext(filename)
    if err != nil {
        return fmt.Errorf("faield to fetch commit ctx: %v", err)
    }

    // jiras
    if len(entry.Jiras) == 0 {
        jiraMap := make(map[string]bool)
        r := regexp.MustCompile(`[a-zA-Z]+-\d+`)
        jiras := r.FindAllString(ctx.PullCtx.Body, -1)
        for _, jira := range jiras {
            if !jiraMap[jira] {
                entry.Jiras = append(entry.Jiras, jira)
                jiraMap[jira] = true
            }
        }
    }
    for _, jiraId := range entry.Jiras {
        jira := Jira{
            ID:   jiraId,
            Link: JiraBaseUrl + jiraId,
        }
        entry.ParsedJiras = append(entry.ParsedJiras, &jira)
    }

    // githubs
    if len(entry.Githubs) == 0 {
        entry.Githubs = entry.Prs
    }
    if len(entry.Githubs) == 0 {
        entry.Githubs = append(entry.Githubs, ctx.PullCtx.Number)
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

func collect() (*Data, error) {
    path := filepath.Join(repoPath, changelogPath)
    files, err := os.ReadDir(path)
    if err != nil {
        return nil, err
    }

    data := &Data{
        System: system,
        Type:   make(map[string][]ScopeEntries),
    }

    maps := make(map[string]map[string][]*ChangelogEntry)

    for i, file := range files {
        if file.IsDir() || !isYAML(file.Name()) {
            log.Printf("Skipping file: %s (%d/%d)", file.Name(), i + 1, len(files))
            continue
        }

        content, err := os.ReadFile(filepath.Join(path, file.Name()))
        if err != nil {
            return nil, err
        }

        log.Printf("Processing file: %s (%d/%d)", file.Name(), i + 1, len(files))

        // parse entry
        entry := &ChangelogEntry{}
        err = yaml.Unmarshal(content, entry)

        if err != nil {
            return nil, fmt.Errorf("failed to unmarshal YAML from %s: %v", file.Name(), err)
        }

        err = processEntry(file.Name(), entry)
        if err != nil {
            return nil, fmt.Errorf("fialed to process entry: %v", err)
        }

        if maps[entry.Type] == nil {
            maps[entry.Type] = make(map[string][]*ChangelogEntry)
        }
        maps[entry.Type][entry.Scope] = append(maps[entry.Type][entry.Scope], entry)
    }

    data.Type = make(map[string][]ScopeEntries)
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

func generate(data *Data) (string, error) {
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
        "trim": strings.TrimSpace,
    }).ParseFiles("changelog-markdown.tmpl")
    if err != nil {
        panic(err)
    }
    err = tmpl.Execute(os.Stdout, data)
    if err != nil {
        panic(err)
    }

    return "", nil
}

func main() {
    token = os.Getenv("GITHUB_TOKEN")
    if token == "" {
        log.Fatal("GITHUB_TOKEN environment variable is missing")
    }

    var app = cli.App{
        Name:    "Kong changelog generator",
        Version: "1.0.0",
        Commands: []*cli.Command{
            // generate command
            {
                Name:  "generate",
                Usage: "Generate changelog",
                Flags: []cli.Flag{
                    &cli.StringFlag{
                        Name:     "changelog_path",
                        Usage:    "The changelog path under repo_path (relative). (e.g. changelog/unreleased/kong)",
                        Required: true,
                    },
                    &cli.StringFlag{
                        Name:     "system",
                        Usage:    "The software name. (e.g. Kong)",
                        Required: true,
                    },
                    &cli.StringFlag{
                        Name:     "repo_path",
                        Usage:    "The repository path (full). (e.g. /path/to/your/repository)",
                        Required: true,
                    },
                    &cli.StringFlag{
                        Name:     "repo",
                        Usage:    "The repository ORG/NAME under GitHub. (e.g. Kong/kong)",
                        Required: true,
                    },
                },
                Action: func(c *cli.Context) error {
                    repoPath = c.String("repo_path")
                    changelogPath = c.String("changelog_path")
                    system = c.String("system")
                    repo = c.String("repo")

                    data, err := collect()
                    if err != nil {
                        return err
                    }
                    data.System = system
                    changelog, err := generate(data)
                    _ = changelog
                    return err
                },
            },
        },
    }
    if err := app.Run(os.Args); err != nil {
        log.Fatal(err)
    }
}
