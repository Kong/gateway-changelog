# Gateway Changelog Toolchain

This repo contains the scripts and Actions needed to validate/maintain/generate
`CHANGELOG.md` from Gateway repos.

# Format

Changelog files are individual YAML format files, filename should typically
be a few word description of what has been changed. e.g. `request_id.yml` or
`bump_openssl.yml`.

Changelog files only need to be unique within a single release, different
releases could have changelog files with the same name and not cause issues.

The content of the YAML file is as follows:

```yaml
message: # "Description of your change" (required)
type: # One of "feature", "bugfix", "dependency", "deprecation", "breaking_change", "performance" (required)
scope: # One of "Core", "Plugin", "PDK", "Admin API", "Performance", "Configuration", "Clustering", "Portal", "CLI Command" (optional)
```

**Examples:**

```yaml
message: "Bumped OpenResty from x.x.x.x to x.x.x.x"
type: dependency
```

```yaml
message: Fix an issue that foo does not work correctly
type: bugfix
scope: Core
```

`message` and `type` are **required**, `scope` could be omitted for changes
that has no meaningful scope (e.g. dependency bumps).

# Changelog generator

To use this tool to generate a changelog, first you need to have a GitHub PAT
(classic or fine-grained) that allows access to the repository which you are
generating changelog for.

If you are using fine-grained PAT, then make sure this PAT has access to the
target repo and has **Read** access to "Contents", "Issues", "Metadata" and
"Pull requests" of the target repo.

Make sure the PAT is set as the `GITHUB_TOKEN` environment variable.

To generate changelog for [Kong/kong](https://github.com/Kong/kong), run the following:

```shell
./changelog generate --changelog_path changelog/unreleased/kong --system Kong --repo_path /path/to/cloned/kong/kong --repo Kong/kong > CHANGELOG.md
```

# License

```
Copyright 2023 Kong Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```
